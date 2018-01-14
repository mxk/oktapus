package cmd

import (
	"bytes"
	crand "crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"encoding/gob"
	"encoding/json"
	"errors"
	"math"
	"math/rand"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/LuminalHQ/oktapus/awsgw"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/service/iam"
)

// Do not ask about this.
const ctlRole = "TheOktapusIsComingForYou"

// errCtlUpdate indicates that the Save() operation was interrupted and the
// current account control information was not saved.
var errCtlUpdate = errors.New("account update interrupted")

// errNoCtl indicates missing control information.
var errNoCtl = errors.New("control information not available")

// Account is an account in an AWS organization.
type Account struct {
	*awsgw.Account
	*Ctl

	IAM *iam.IAM
	Err error

	// TODO: Store active/unmodified Ctl information
}

// Creds returns temporary account credentials.
func (ac *Account) Creds() *credentials.Credentials {
	if ac.IAM != nil {
		return ac.IAM.Config.Credentials
	}
	return nil
}

// Accounts is a group of accounts that can be operated on in parallel.
type Accounts []*Account

// Sort sorts accounts by name.
func (s Accounts) Sort() Accounts {
	sort.Sort(byName(s))
	return s
}

// Shuffle randomizes account order.
func (s Accounts) Shuffle() Accounts {
	if len(s) > math.MaxInt32 {
		panic("you have way too many accounts")
	}
	var b [8]byte
	if _, err := crand.Read(b[:]); err != nil {
		panic(err)
	}
	seed := int64(binary.LittleEndian.Uint64(b[:]))
	rng := rand.New(rand.NewSource(seed))
	for i := int32(len(s) - 1); i > 0; i-- {
		j := rng.Int31n(i + 1)
		s[i], s[j] = s[j], s[i]
	}
	return s
}

// RequireIAM ensures that all accounts have an IAM client.
func (s Accounts) RequireIAM(c *awsgw.Client) Accounts {
	sess := c.ConfigProvider()
	var cfg aws.Config
	for _, ac := range s {
		if ac.IAM == nil {
			cfg.Credentials = c.Creds(ac.ID)
			ac.IAM = iam.New(sess, &cfg)
		}
	}
	return s
}

// RequireCtl ensures that all accounts have control information. Existing
// information is not refreshed.
func (s Accounts) RequireCtl() Accounts {
	var refresh Accounts
	for i, ac := range s {
		if ac.Ctl == nil {
			if len(refresh) == 0 {
				refresh = make(Accounts, 0, len(s)-i)
			}
			refresh = append(refresh, ac)
		}
	}
	if len(refresh) > 0 {
		refresh.RefreshCtl()
	}
	return s
}

// RefreshCtl retrieves current control information for all accounts.
func (s Accounts) RefreshCtl() Accounts {
	return s.Apply(func(ac *Account) {
		ctl := ac.Ctl
		ac.Ctl = nil
		in := iam.GetRoleInput{RoleName: aws.String(ctlRole)}
		var out *iam.GetRoleOutput
		if out, ac.Err = ac.IAM.GetRole(&in); ac.Err == nil {
			if ctl == nil {
				ctl = new(Ctl)
			}
			if ac.Err = ctl.decode(out.Role.Description); ac.Err == nil {
				ac.Ctl = ctl
			}
		}
	})
}

// Save updates control information for all accounts.
func (s Accounts) Save(init bool) Accounts {
	return s.Apply(func(ac *Account) {
		if ac.Ctl == nil {
			if ac.Err == nil {
				ac.Err = errNoCtl
			}
			return
		}
		// TODO: Get current control information and compare
		// TODO: Init
		var upd string
		if upd, ac.Err = ac.Ctl.encode(); ac.Err != nil {
			return
		}
		in := iam.UpdateRoleDescriptionInput{
			Description: aws.String(upd),
			RoleName:    aws.String(ctlRole),
		}
		var out *iam.UpdateRoleDescriptionOutput
		if out, ac.Err = ac.IAM.UpdateRoleDescription(&in); ac.Err != nil {
			return
		}
		if aws.StringValue(out.Role.Description) != upd {
			ac.Err = errCtlUpdate
		}
	})
}

// Apply executes fn on each account in parallel.
func (s Accounts) Apply(fn func(ac *Account)) Accounts {
	// The number of goroutines is fixed because the work is IO-bound. It simply
	// sets the number of API requests that can be in-flight at any given time.
	n := 10
	if len(s) < n {
		if n = len(s); n == 0 {
			return s
		}
	}
	var wg sync.WaitGroup
	ch := make(chan *Account, n)
	for i := n; i > 0; i-- {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for ac := range ch {
				fn(ac)
			}
		}()
	}
	for _, ac := range s {
		ch <- ac
	}
	close(ch)
	wg.Wait()
	return s
}

// byName implements sort.Interface to sort accounts by name.
type byName Accounts

func (s byName) Len() int      { return len(s) }
func (s byName) Swap(i, j int) { s[i], s[j] = s[j], s[i] }
func (s byName) Less(i, j int) bool {
	if s[i].Name == s[j].Name {
		return s[i].ID < s[j].ID
	}
	return s[i].Name < s[j].Name
}

// TODO: Add MAC?

// Ctl contains account control information.
type Ctl struct {
	Desc  string
	Owner string
	Tags  []string
}

// decode deserializes ctl from a base64-encoded string.
func (ctl *Ctl) decode(src *string) error {
	*ctl = Ctl{}
	if src == nil || *src == "" {
		return nil
	}
	s, ver := *src, 0
	if i := strings.IndexByte(s, '#'); i > 0 {
		if v, err := strconv.Atoi(s[0:i]); err == nil {
			s, ver = s[i+1:], v
		}
	}
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return err
	}
	switch ver {
	case 1:
		err = json.Unmarshal(b, &ctl)
	default:
		err = gob.NewDecoder(bytes.NewReader(b)).Decode(&ctl)
	}
	if err != nil {
		*ctl = Ctl{}
	}
	return err
}

const encVer = "1#"

// encode serializes ctl into a base64-encoded string.
func (ctl *Ctl) encode() (string, error) {
	sort.Strings(ctl.Tags)
	// JSON encoding is slightly slower but more compact than gob, and provides
	// better interoperability with non-Go clients.
	b, err := json.Marshal(ctl)
	if err != nil {
		return "", err
	}
	enc := base64.StdEncoding
	buf := make([]byte, len(encVer)+enc.EncodedLen(len(b)))
	enc.Encode(buf[copy(buf, encVer):], b)
	return string(buf), nil
}
