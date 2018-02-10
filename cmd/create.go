package cmd

import (
	"bufio"
	"encoding/gob"
	"flag"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/LuminalHQ/oktapus/awsgw"
	"github.com/LuminalHQ/oktapus/internal"
	"github.com/LuminalHQ/oktapus/op"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/service/iam"
	"github.com/aws/aws-sdk-go/service/iam/iamiface"
	orgs "github.com/aws/aws-sdk-go/service/organizations"
)

func init() {
	op.Register(&op.CmdInfo{
		Names:   []string{"create"},
		Summary: "Create new account(s)",
		Usage:   "[options] num account-name root-email",
		MinArgs: 3,
		MaxArgs: 3,
		New:     func() op.Cmd { return &create{Name: "create"} },
	})
	gob.Register([]*newAccountsOutput{})
}

type create struct {
	Name
	PrintFmt
	Exec     bool
	Num      int
	NameTpl  string
	EmailTpl string
}

func (cmd *create) Help(w *bufio.Writer) {
	op.WriteHelp(w, `
		Create new accounts.

		WARNING: Accounts can never be deleted. They can be closed and removed
		from your organization, but doing so requires gaining access to the root
		user via the email password reset procedure. The rest of the process is
		also entirely manual, requiring many mouse clicks. Don't create new
		accounts unless you really need them for the long-term.

		Each account must have a unique name and email address. The command
		accepts templates for both, and uses automatic numbering to generate
		unique names and emails. Many email providers treat addresses in the
		form user+extratext@example.com as an alias for user@example.com, so
		this is a convenient way of generating unique, but valid email
		addresses. This address may be needed later to reset the root password.

		The templates use a dynamic field in the format '{N}' where N is a
		number. If N is 0 or absent, the starting value is automatically
		selected based on existing account names. Otherwise, the numbering
		begins with N. You can use leading zeros to set field width. For
		example, the template 'test-{00}' will generate account names 'test-01',
		'test-02', and so on.

		Unless the -exec option is specified, the command simply returns the
		account names and emails that would be used if the accounts were
		actually created.
	`)
}

type newAccountsOutput struct{ Name, Email string }

func (cmd *create) FlagCfg(fs *flag.FlagSet) {
	cmd.PrintFmt.FlagCfg(fs)
	fs.BoolVar(&cmd.Exec, "exec", false,
		"Execute account creation (list names/emails otherwise)")
}

func (cmd *create) Run(ctx *op.Ctx, args []string) error {
	n, err := strconv.Atoi(args[0])
	cmd.NameTpl, cmd.EmailTpl = args[1], args[2]
	if err != nil {
		op.UsageErr(cmd, "first argument must be a number")
	} else if n <= 0 {
		op.UsageErr(cmd, "number of accounts must be > 0")
	} else if n > 50 {
		op.UsageErr(cmd, "number of accounts must be <= 50")
	} else if i := strings.IndexByte(cmd.EmailTpl, '@'); i == -1 {
		op.UsageErr(cmd, "invalid email address")
	}
	cmd.Num = n
	out, err := ctx.Call(cmd)
	if err == nil {
		err = cmd.Print(out)
	}
	return err
}

func (cmd *create) Call(ctx *op.Ctx) (interface{}, error) {
	// Only the organization's master account can create new accounts
	c := ctx.AWS()
	if !c.IsMaster() {
		return nil, fmt.Errorf("gateway account (%s) is not org master (%s)",
			c.Ident().AccountID, c.OrgInfo().MasterAccountID)
	}

	// Configure name/email counters
	n := cmd.Num
	nameCtr, nameErr := newCounter(cmd.NameTpl)
	emailCtr, emailErr := newCounter(cmd.EmailTpl)
	if nameErr != nil {
		op.UsageErr(cmd, nameErr.Error())
	} else if emailErr != nil {
		op.UsageErr(cmd, emailErr.Error())
	} else if (nameCtr == nil) != (emailCtr == nil) {
		op.UsageErr(cmd, "account name/email format mismatch")
	} else if nameCtr != nil {
		if err := c.Refresh(); err != nil {
			return nil, err
		}
		setCounters(c.Accounts(), nameCtr, emailCtr)
	} else if n > 1 {
		op.UsageErr(cmd, "account name/email must be dynamic templates")
	} else {
		nameCtr, emailCtr = &counter{p: cmd.NameTpl}, &counter{p: cmd.EmailTpl}
	}

	// Create accounts
	in := make(chan *orgs.CreateAccountInput)
	var ls []*newAccountsOutput
	go func() {
		defer close(in)
		for ; n > 0; n-- {
			if cmd.Exec {
				in <- &orgs.CreateAccountInput{
					AccountName: aws.String(nameCtr.String()),
					Email:       aws.String(emailCtr.String()),
					RoleName:    aws.String(c.CommonRole),
				}
			} else {
				ls = append(ls, &newAccountsOutput{
					Name:  nameCtr.String(),
					Email: emailCtr.String(),
				})
			}
			nameCtr.n++
			emailCtr.n++
		}
	}()
	out := op.CreateAccounts(c.OrgsClient(), in)

	// Configure accounts
	var wg sync.WaitGroup
	acs := make(op.Accounts, 0, n)
	masterID := c.OrgInfo().MasterAccountID
	for r := range out {
		if r.Err != nil {
			ac := op.NewAccount("", aws.StringValue(r.Name))
			ac.Err = r.Err
			acs = append(acs, ac)
			continue
		}
		info := c.Update(r.Account)
		ac := op.NewAccount(info.ID, info.Name)
		ac.Init(c.ConfigProvider(), c.CredsProvider(ac.ID))
		acs = append(acs, ac)
		wg.Add(1)
		go func(ac *op.Account) {
			defer wg.Done()

			// Wait for credentials to become valid
			if ac.Err = waitForCreds(ac); ac.Err != nil {
				return
			}

			// Create OrganizationAccountAccessRole
			if ac.Err = createOrgAccessRole(ac.IAM(), masterID); ac.Err != nil {
				return
			}

			// TODO: Replace inline AdministratorAccess policy with attached one
			// for the initial role.

			// Initialize account control information
			ac.Ctl = &op.Ctl{Tags: op.Tags{"new"}}
			ac.Err = ac.Ctl.Init(ac.IAM())
		}(ac)
	}
	wg.Wait()
	if !cmd.Exec {
		return ls, nil
	}
	return listResults(acs.Sort()), nil
}

// counter generates strings with a single dynamic int field.
type counter struct {
	p string // printf format
	s string // scanf format
	n int    // current value
}

// newCounter returns a counter for template string s. It returns nil if s is
// not a valid template.
func newCounter(s string) (*counter, error) {
	i, j := strings.IndexByte(s, '{'), strings.LastIndexByte(s, '}')
	if i == -1 && j == -1 {
		return nil, nil
	} else if j <= i {
		return nil, fmt.Errorf("invalid counter template %q", s)
	}
	n, err := strconv.Atoi(s[i+1 : j])
	if err != nil && i+1 != j {
		return nil, fmt.Errorf("invalid counter template %q (%v)", s, err)
	}
	prec := j - i - 1
	if s[i+1] == '-' {
		prec--
	}
	return &counter{
		p: fmt.Sprintf("%s%%.%dd%s", s[:i], prec, s[j+1:]),
		s: fmt.Sprintf("%s%%d%s\n", s[:i], s[j+1:]),
		n: n,
	}, nil
}

// scan extracts the counter value from string s.
func (c *counter) scan(s string) (int, bool) {
	var n int
	_, err := fmt.Sscanf(s, c.s, &n)
	return n, err == nil
}

// String implements fmt.Stringer interface.
func (c *counter) String() string {
	if c.s != "" {
		return fmt.Sprintf(c.p, c.n)
	}
	return c.p
}

// setCounters sets initial values for name and email counters based on existing
// account names, but only if explicit starting values were not specified.
func setCounters(acs []*awsgw.Account, name, email *counter) {
	if name.n == 0 {
		for _, ac := range acs {
			if n, ok := name.scan(ac.Name); ok && name.n < n {
				name.n = n
			}
		}
		name.n++
	}
	if email.n == 0 {
		email.n = name.n
	}
}

// waitForCreds blocks until account credentials become valid.
func waitForCreds(ac *op.Account) error {
	timeout := internal.Time().Add(time.Minute)
	for {
		_, err := ac.Creds(true)
		e, ok := err.(awserr.RequestFailure)
		if err == nil || !ok || e.StatusCode() != http.StatusForbidden ||
			!internal.Time().Before(timeout) {
			return err
		}
		time.Sleep(time.Second)
	}
}

// createOrgAccessRole creates OrganizationAccountAccessRole. Role creation
// order is reversed because the user creating a new account may not be able to
// assume the default OrganizationAccountAccessRole.
func createOrgAccessRole(c iamiface.IAMAPI, masterAccountID string) error {
	assumeRolePolicy := op.NewAssumeRolePolicy(masterAccountID)
	accessPolicy := op.Policy{Statement: []*op.Statement{{
		Effect:   "Allow",
		Action:   op.PolicyMultiVal{"*"},
		Resource: op.PolicyMultiVal{"*"},
	}}}
	role := iam.CreateRoleInput{
		AssumeRolePolicyDocument: assumeRolePolicy.Doc(),
		RoleName:                 aws.String("OrganizationAccountAccessRole"),
	}
	policy := iam.PutRolePolicyInput{
		PolicyDocument: accessPolicy.Doc(),
		PolicyName:     aws.String("AdministratorAccess"),
		RoleName:       role.RoleName,
	}
	// New credentials for a new account sometimes result in
	// InvalidClientTokenId error for the first few seconds.
	timeout := internal.Time().Add(30 * time.Second)
	for {
		_, err := c.CreateRole(&role)
		if err == nil {
			_, err = c.PutRolePolicy(&policy)
			return err
		}
		if !awsErrCode(err, "InvalidClientTokenId") ||
			!internal.Time().Before(timeout) {
			return err
		}
		time.Sleep(time.Second)
	}
}
