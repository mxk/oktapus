package cmd

import (
	"bytes"
	crand "crypto/rand"
	"encoding/base64"
	"encoding/gob"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/big"
	"math/rand"
	"sort"

	"github.com/LuminalHQ/oktapus/awsgw"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/iam"
	"strings"
	"strconv"
)

// TODO: Check encoding length and compare with JSON

func init() {
	// Seed random number generator
	// TODO: Should be math.MaxInt64 + 1
	seed, err := crand.Int(crand.Reader, big.NewInt(math.MaxInt64))
	if err != nil {
		panic(err)
	}
	rand.Seed(seed.Int64())
}

// Do not ask about this.
const ctlRole = "TheOktapusIsComingForYou"

// errCtlUpdate indicates that the Save() operation was interrupted and the
// current account control information was not saved.
var errCtlUpdate = errors.New("account update interrupted")

// Account is an account in an AWS organization.
type Account struct {
	*awsgw.Account
	// TODO: Alias?

	iam *iam.IAM
	c   *awsgw.Client

	ctl    Ctl
	cached bool
	err    error
}

// getAccounts returns all accounts in the organization that match the spec.
func getAccounts(c *awsgw.Client, spec string) ([]*Account, error) {
	all := c.Accounts()
	if len(all) == 0 {
		if err := c.Refresh(); err != nil {
			return nil, err
		}
		all = c.Accounts()
	}
	match := make([]*Account, len(all))
	for i, ac := range all {
		match[i] = &Account{Account: ac, c: c}
	}
	err := newAccountSpec(spec, c.CommonRole).Filter(&match)
	sort.Sort(byName(match))
	return match, err
}

// shuffle randomizes account order.
func shuffle(v []*Account) {
	// TODO: rand.Shuffle is coming
	for i := len(v) - 1; i > 0; i-- {
		j := rand.Int31n(int32(i + 1))
		v[i], v[j] = v[j], v[i]
	}
}

// IAM returns an IAM client for the account.
func (ac *Account) IAM() *iam.IAM {
	if ac.iam == nil {
		ac.iam = ac.c.IAM(ac.ID)
	}
	return ac.iam
}

// Init initializes account control information.
func (ac *Account) Init() error {
	// TODO: Figure out a more restricted policy
	policy := fmt.Sprintf(awsgw.AssumeRolePolicy, ac.c.AccountID)
	// TODO: Can path be used for better organization?
	in := iam.CreateRoleInput{
		AssumeRolePolicyDocument: aws.String(policy),
		RoleName:                 aws.String(ctlRole),
	}
	_, err := ac.IAM().CreateRole(&in)
	// TODO: If AlreadyExists error, update ac.ctl
	if err == nil {
		ac.ctl = Ctl{}
		ac.cached = true
		ac.err = nil
	}
	return err
}

// Refresh updates account control information.
func (ac *Account) Refresh() error {
	in := iam.GetRoleInput{RoleName: aws.String(ctlRole)}
	var out *iam.GetRoleOutput
	out, ac.err = ac.IAM().GetRole(&in)
	if ac.cached = true; ac.err == nil {
		ac.err = ac.ctl.decode(out.Role.Description)
	}
	return ac.err
}

// Error returns the most recent refresh error, if any.
func (ac *Account) Error() error {
	return ac.err
}

// Ctl returns cached account control information, refreshing it on first
// access. It always returns a valid pointer. Use Error() to get error
// information.
func (ac *Account) Ctl() *Ctl {
	if !ac.cached {
		ac.Refresh()
	}
	return &ac.ctl
}

// TODO: Test concurrent Save calls and figure out if they are atomic. If not
// determine the necessary delay before a Refresh() to get current status.

// Save updates account control information. This overwrites current control
// information, even if it no longer matches the cached copy.
func (ac *Account) Save() error {
	desc, err := ac.ctl.encode()
	if err != nil {
		return err
	}
	in := iam.UpdateRoleDescriptionInput{
		Description: aws.String(desc),
		RoleName:    aws.String(ctlRole),
	}
	out, err := ac.IAM().UpdateRoleDescription(&in)
	ac.cached = err == nil
	if ac.cached && aws.StringValue(out.Role.Description) != desc {
		err = errCtlUpdate
		ac.err = ac.ctl.decode(out.Role.Description)
	}
	return err
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
