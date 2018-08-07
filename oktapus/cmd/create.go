package cmd

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/LuminalHQ/cloudcover/oktapus/account"
	"github.com/LuminalHQ/cloudcover/oktapus/awsx"
	"github.com/LuminalHQ/cloudcover/oktapus/creds"
	"github.com/LuminalHQ/cloudcover/oktapus/op"
	"github.com/LuminalHQ/cloudcover/x/cli"
	"github.com/LuminalHQ/cloudcover/x/fast"
	"github.com/LuminalHQ/cloudcover/x/iamx"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	orgs "github.com/aws/aws-sdk-go-v2/service/organizations"
)

// accountSetupRole is the name of a temporary role created by CreateAccount.
// This role is deleted after initial account configuration. The common role is
// not used for this purpose because CreateAccount cannot create roles with a
// path component.
const accountSetupRole = "OktapusAccountSetup"

var createCli = register(&cli.Info{
	Name:    "create",
	Usage:   "[options] num account-name root-email",
	Summary: "Create new account(s)",
	MinArgs: 3,
	MaxArgs: 3,
	New:     func() cli.Cmd { return &createCmd{} },
})

type createCmd struct {
	OutFmt
	Exec     bool `flag:"Execute account creation (list names/emails otherwise)"`
	Num      int
	NameTpl  string
	EmailTpl string
}

func (cmd *createCmd) Info() *cli.Info { return createCli }

func (cmd *createCmd) Help(w *cli.Writer) {
	w.Text(`
	Create new accounts.

	WARNING: Accounts can never be deleted. They can be closed and removed from
	your organization, but doing so requires gaining access to the root user via
	the email password reset procedure. The rest of the process is also entirely
	manual, requiring many mouse clicks. Don't create new accounts unless you
	really need them for the long-term.

	Each account must have a unique name and email address. The command accepts
	templates for both, and uses automatic numbering to generate unique names
	and emails. Many email providers treat addresses in the form
	user+extratext@example.com as an alias for user@example.com, so this is a
	convenient way of generating unique, but valid email addresses. This address
	may be needed later to reset the root password.

	The templates use a dynamic field in the format '{N}' where N is a number.
	If N is 0 or absent, the starting value is automatically selected based on
	existing account names. Otherwise, the numbering begins with N. You can use
	leading zeros to set field width. For example, the template 'test-{00}' will
	generate account names 'test-01', 'test-02', and so on.

	Unless the -exec option is specified, the command simply returns the account
	names and emails that would be used if the accounts were actually created.
	`)
}

type newAccountsOutput struct{ Name, Email string }

func (cmd *createCmd) Main(args []string) error {
	return cmd.Run(op.NewCtx(), args)
}

func (cmd *createCmd) Run(ctx *op.Ctx, args []string) error {
	n, err := strconv.Atoi(args[0])
	cmd.NameTpl, cmd.EmailTpl = args[1], args[2]
	if err != nil {
		return cli.Error("first argument must be a number")
	} else if n <= 0 {
		return cli.Error("number of accounts must be > 0")
	} else if n > 50 {
		return cli.Error("number of accounts must be <= 50")
	} else if i := strings.IndexByte(cmd.EmailTpl, '@'); i == -1 {
		return cli.Error("invalid email address")
	}
	cmd.Num = n
	out, err := ctx.Call(cmd)
	if err == nil {
		err = cmd.Print(out)
	}
	return err
}

func (cmd *createCmd) Call(ctx *op.Ctx) (interface{}, error) {
	// Only the organization's master account can create new accounts
	gw := ctx.Gateway()
	if !gw.IsMaster() {
		return nil, fmt.Errorf("gateway account (%s) is not org master (%s)",
			gw.Ident().Account, gw.Org().MasterID)
	}

	// Configure name/email counters
	n := cmd.Num
	nameCtr, nameErr := newCounter(cmd.NameTpl)
	emailCtr, emailErr := newCounter(cmd.EmailTpl)
	if nameErr != nil {
		return nil, cli.Error(nameErr)
	} else if emailErr != nil {
		return nil, cli.Error(emailErr)
	} else if (nameCtr == nil) != (emailCtr == nil) {
		return nil, cli.Error("account name/email format mismatch")
	} else if nameCtr != nil {
		if err := gw.Refresh(); err != nil {
			return nil, err
		}
		setCounters(gw.Accounts(), nameCtr, emailCtr)
	} else if n > 1 {
		return nil, cli.Error("account name/email must be dynamic templates")
	} else {
		nameCtr, emailCtr = &counter{p: cmd.NameTpl}, &counter{p: cmd.EmailTpl}
	}

	// Create accounts
	in := make([]*orgs.CreateAccountInput, n)
	for i := range in {
		in[i] = &orgs.CreateAccountInput{
			AccountName: aws.String(nameCtr.String()),
			Email:       aws.String(emailCtr.String()),
			RoleName:    aws.String(accountSetupRole),
		}
		nameCtr.n++
		emailCtr.n++
	}
	if !cmd.Exec {
		out := make([]*newAccountsOutput, len(in))
		for i, ac := range in {
			out[i] = &newAccountsOutput{Name: *ac.AccountName, Email: *ac.Email}
		}
		return out, nil
	}
	out := awsx.CreateAccounts(*orgs.New(*ctx.Cfg()), in)

	// Configure accounts
	var wg sync.WaitGroup
	acs := make(op.Accounts, 0, n)
	masterID := gw.Org().MasterID
	setupRoleARN := gw.CommonRole.WithPathName(accountSetupRole)
	for r := range out {
		if r.Err != nil {
			ac := op.NewAccount("", aws.StringValue(r.Name))
			ac.Err = r.Err
			acs = append(acs, ac)
			continue
		}
		info := gw.AddAccount(r.Account)
		ac := op.NewAccount(info.ID, info.DisplayName())
		acs = append(acs, ac)
		wg.Add(1)
		go func(ac *op.Account, setupCreds, commonCreds *creds.Provider) {
			defer wg.Done()

			// Wait for setup credentials to become valid
			ac.Init(ctx.Cfg(), setupCreds)
			if ac.Err = waitForCreds(ac); ac.Err != nil {
				return
			}

			// Create admin and common roles
			orgRoleErr := createOrgAccessRole(ac.IAM(), masterID)
			crPath, crName := gw.CommonRole.Path(), gw.CommonRole.Name()
			ac.Err = createCommonRole(ac.IAM(), masterID, crPath, crName)
			if ac.Err != nil {
				return
			} else if orgRoleErr != nil {
				ac.Err = orgRoleErr
				return
			}

			// Switch to common role credentials
			ac.Init(ctx.Cfg(), commonCreds)
			if ac.Err = waitForCreds(ac); ac.Err != nil {
				return
			}

			// Delete setup role
			if err := ac.IAM().DeleteRole(accountSetupRole); err != nil {
				log.W("Failed to delete %s role in account %s: %v",
					accountSetupRole, ac.ID, err)
			}

			// Initialize account control information
			ac.Ctl = &op.Ctl{Tags: op.Tags{"new"}}
			ac.Err = ac.Ctl.Init(ac.IAM())
		}(ac, gw.AssumeRole(setupRoleARN.WithAccount(ac.ID)), gw.CredsProvider(ac.ID))
	}
	wg.Wait()
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
func setCounters(acs []*account.Info, name, email *counter) {
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
	timeout := fast.Time().Add(time.Minute)
	for {
		err := ac.CredsProvider().Ensure(-1)
		if err == nil || !awsx.IsStatus(err, http.StatusForbidden) ||
			!fast.Time().Before(timeout) {
			return err
		}
		fast.Sleep(time.Second)
	}
}

// createOrgAccessRole creates OrganizationAccountAccessRole. Role creation
// order is reversed because the user creating a new account may not be able to
// assume the default OrganizationAccountAccessRole.
func createOrgAccessRole(c iamx.Client, masterAccountID string) error {
	assumeRolePolicy := iamx.AssumeRolePolicy(masterAccountID)
	accessPolicy := iamx.Policy{Statement: []*iamx.Statement{{
		Effect:   "Allow",
		Action:   iamx.PolicyMultiVal{"*"},
		Resource: iamx.PolicyMultiVal{"*"},
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
	timeout := fast.Time().Add(30 * time.Second)
	for {
		_, err := c.CreateRoleRequest(&role).Send()
		if err == nil {
			_, err = c.PutRolePolicyRequest(&policy).Send()
			return err
		}
		if !awsx.IsCode(err, "InvalidClientTokenId") ||
			!fast.Time().Before(timeout) {
			return err
		}
		fast.Sleep(time.Second)
	}
}

// createCommonRole creates the common role that replaces accountSetupRole.
func createCommonRole(c iamx.Client, masterAccountID, path, name string) error {
	assumeRolePolicy := iamx.AssumeRolePolicy(masterAccountID)
	role := iam.CreateRoleInput{
		AssumeRolePolicyDocument: assumeRolePolicy.Doc(),
		Path:     aws.String(path),
		RoleName: aws.String(name),
	}
	policy := iam.AttachRolePolicyInput{
		PolicyArn: aws.String("arn:aws:iam::aws:policy/AdministratorAccess"),
		RoleName:  role.RoleName,
	}
	_, err := c.CreateRoleRequest(&role).Send()
	if err == nil {
		_, err = c.AttachRolePolicyRequest(&policy).Send()
	}
	return err
}
