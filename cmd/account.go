package cmd

import (
	"bytes"
	"encoding/base64"
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

// ctlPath is a common path for automatically created IAM users and roles.
const ctlPath = "/oktapus/"

// ctlPolicy denies everyone the ability to assume the account control
// information role.
const ctlPolicy = `{
	"Version": "2012-10-17",
	"Statement": [{
		"Effect": "Deny",
		"Principal": {"AWS": "*"},
		"Action": "sts:AssumeRole"
	}]
}`

// errCtlUpdate indicates new account control information was not saved.
var errCtlUpdate = errors.New("account control information update interrupted")

// errNoCtl indicates missing account control information.
var errNoCtl = errors.New("account control information not available")

// Account is an account in an AWS organization.
type Account struct {
	*awsgw.Account
	*Ctl

	IAM *iam.IAM
	Err error

	ref Ctl
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
	for i := int32(len(s) - 1); i > 0; i-- {
		j := rand.Int31n(i + 1)
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
		if ac.Ctl == nil && ac.Err == nil {
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
		if ac.Err = ac.ref.get(ac.IAM); ac.Err != nil {
			ac.Ctl = nil
		} else {
			if ac.Ctl == nil {
				ac.Ctl = new(Ctl)
			}
			*ac.Ctl = ac.ref
		}
	})
}

// Save updates control information for all accounts. When changing the owner,
// the caller must refresh account control information after a delay to confirm
// owner status.
func (s Accounts) Save() Accounts {
	return s.Apply(func(ac *Account) {
		if ac.Ctl == nil {
			if ac.Err == nil {
				ac.Err = errNoCtl
			}
			return
		}

		// Get current state and merge changes
		var cur Ctl
		if ac.Err = cur.get(ac.IAM); ac.Err != nil {
			return
		} else if ac.merge(&cur, &ac.ref); cur.eq(ac.Ctl) {
			ac.ref = cur
			return // Nothing to do
		}

		// To change the owner, current and reference states must match
		if cur.Owner != ac.Owner && cur.Owner != ac.ref.Owner {
			ac.Err, ac.ref = errCtlUpdate, cur
			return
		}

		// Update state
		if ac.Err = ac.Ctl.set(ac.IAM); ac.Err != nil {
			ac.ref = cur
		} else {
			ac.ref = *ac.Ctl
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

// Ctl contains account control information.
type Ctl struct {
	Desc  string
	Owner string
	Tags  Tags
}

// eq returns true if ctl == other.
func (ctl *Ctl) eq(other *Ctl) bool {
	return ctl == other || (ctl != nil && other != nil &&
		ctl.Desc == other.Desc && ctl.Owner == other.Owner &&
		ctl.Tags.eq(other.Tags))
}

// merge performs a 3-way merge of account control information changes.
func (ctl *Ctl) merge(cur, ref *Ctl) {
	if ctl.Owner == ref.Owner {
		ctl.Owner = cur.Owner
	}
	if ctl.Desc == ref.Desc {
		ctl.Desc = cur.Desc
	}
	set, clr := ctl.Tags.diff(ref.Tags)
	ctl.Tags = append(ctl.Tags[:0], cur.Tags...)
	ctl.Tags.apply(set, clr)
}

// init creates account control information in an uncontrolled account.
func (ctl *Ctl) init(c *iam.IAM) error {
	return ctl.exec(c, func(b64 string) (*iam.Role, error) {
		in := iam.CreateRoleInput{
			AssumeRolePolicyDocument: aws.String(ctlPolicy),
			Description:              aws.String(b64),
			Path:                     aws.String(ctlPath),
			RoleName:                 aws.String(ctlRole),
		}
		out, err := c.CreateRole(&in)
		if out != nil {
			return out.Role, err
		}
		return nil, err
	})
}

// get retrieves current account control information.
func (ctl *Ctl) get(c *iam.IAM) error {
	in := iam.GetRoleInput{RoleName: aws.String(ctlRole)}
	out, err := c.GetRole(&in)
	if err == nil {
		return ctl.decode(out.Role.Description)
	}
	*ctl = Ctl{}
	return err
}

// set stores account control information.
func (ctl *Ctl) set(c *iam.IAM) error {
	return ctl.exec(c, func(b64 string) (*iam.Role, error) {
		in := iam.UpdateRoleDescriptionInput{
			Description: aws.String(b64),
			RoleName:    aws.String(ctlRole),
		}
		out, err := c.UpdateRoleDescription(&in)
		if out != nil {
			return out.Role, err
		}
		return nil, err
	})
}

// exec executes init or set operations.
func (ctl *Ctl) exec(c *iam.IAM, fn func(b64 string) (*iam.Role, error)) error {
	b64, err := ctl.encode()
	if err != nil {
		return err
	}
	r, err := fn(b64)
	if err == nil && aws.StringValue(r.Description) != b64 {
		err = errCtlUpdate
	}
	return err
}

const ctlVer = "1#"

// encode encodes account control information into a base64 string.
func (ctl *Ctl) encode() (string, error) {
	sort.Strings(ctl.Tags)
	b, err := json.Marshal(ctl)
	if err == nil {
		enc := base64.StdEncoding
		b64 := make([]byte, len(ctlVer)+enc.EncodedLen(len(b)))
		enc.Encode(b64[copy(b64, ctlVer):], b)
		return string(b64), nil
	}
	return "", err
}

// decode decodes account control information from a base64 string.
func (ctl *Ctl) decode(s *string) error {
	if *ctl = (Ctl{}); s == nil || *s == "" {
		return nil
	}
	b64, ver := *s, 0
	if i := strings.IndexByte(b64, '#'); i > 0 {
		if v, err := strconv.Atoi(b64[0:i]); err == nil {
			b64, ver = b64[i+1:], v
		}
	}
	b, err := base64.StdEncoding.DecodeString(b64)
	if err == nil {
		switch ver {
		case 1:
			err = json.Unmarshal(b, ctl)
		default: // TODO: Remove
			err = gob.NewDecoder(bytes.NewReader(b)).Decode(ctl)
		}
		if err != nil {
			*ctl = Ctl{}
		}
		sort.Strings(ctl.Tags)
	}
	return err
}
