package cmd

import (
	"flag"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/LuminalHQ/oktapus/awsgw"
	"github.com/LuminalHQ/oktapus/internal"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/service/iam"
	orgs "github.com/aws/aws-sdk-go/service/organizations"
)

func init() {
	register(&Create{command: command{
		name:    []string{"create"},
		summary: "Create new account(s)",
		usage:   "[options] num account-name root-email",
		minArgs: 3,
		maxArgs: 3,
	}})
}

type Create struct {
	command
	exec bool
}

type newAccountsOutput struct{ Name, Email string }

func (cmd *Create) FlagCfg(fs *flag.FlagSet) {
	cmd.command.FlagCfg(fs)
	fs.BoolVar(&cmd.exec, "exec", false,
		"Execute account creation (list names/emails otherwise)")
}

func (cmd *Create) Run(ctx *Ctx, args []string) error {
	n, err := strconv.Atoi(args[0])
	name, email := args[1], args[2]
	if err != nil {
		usageErr(cmd, "first argument must be a number")
	} else if n <= 0 {
		usageErr(cmd, "number of accounts must be > 0")
	} else if n > 50 {
		usageErr(cmd, "number of accounts must be <= 50")
	} else if i := strings.IndexByte(email, '@'); i == -1 {
		usageErr(cmd, "invalid email address")
	}

	// Only the organization's master account can create new accounts
	c := ctx.AWS()
	masterID := c.OrgInfo().MasterAccountID
	if id := c.Ident(); id.AccountID == "" || id.AccountID != masterID {
		return fmt.Errorf("current account (%s) is not org master (%s)",
			id.AccountID, masterID)
	}

	// Configure name/email counters
	nameCtr, nameErr := newCounter(name)
	emailCtr, emailErr := newCounter(email)
	if nameErr != nil {
		usageErr(cmd, nameErr.Error())
	} else if emailErr != nil {
		usageErr(cmd, emailErr.Error())
	} else if (nameCtr == nil) != (emailCtr == nil) {
		usageErr(cmd, "account name/email format mismatch")
	} else if nameCtr != nil {
		if err := c.Refresh(); err != nil {
			return err
		}
		setCounters(c.Accounts(), nameCtr, emailCtr)
	} else if n > 1 {
		usageErr(cmd, "account name/email must be dynamic templates")
	} else {
		nameCtr, emailCtr = &counter{p: name}, &counter{p: email}
	}

	// Create accounts
	in := make(chan *orgs.CreateAccountInput)
	var ls []*newAccountsOutput
	go func() {
		defer close(in)
		for ; n > 0; n-- {
			if cmd.exec {
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
	out := createAccounts(c.OrgClient(), in, n)

	// Configure accounts
	var wg sync.WaitGroup
	acs := make(Accounts, 0, n)
	for r := range out {
		if r.err == nil {
			acs = append(acs, &Account{Account: c.Update(r.Account)})
		} else {
			acs = append(acs, &Account{
				Account: &awsgw.Account{Name: aws.StringValue(r.Name)},
				Err:     err,
			})
			continue
		}
		acs[len(acs)-1:].RequireIAM(c)
		wg.Add(1)
		go func(ac *Account) {
			defer wg.Done()

			// Wait for credentials to become valid
			if ac.Err = waitForCreds(ac); ac.Err != nil {
				return
			}

			// Create OrganizationAccountAccessRole
			if ac.Err = createOrgAccessRole(ac.IAM, masterID); ac.Err != nil {
				return
			}

			// TODO: Replace inline AdministratorAccess policy with attached one
			// for the initial role.

			// Initialize account control information
			ac.Ctl = &Ctl{Tags: Tags{"new"}}
			ac.Err = ac.Ctl.init(ac.IAM)
		}(acs[len(acs)-1])
	}
	wg.Wait()
	if !cmd.exec {
		return cmd.PrintOutput(ls)
	}
	return cmd.PrintOutput(listResults(acs.Sort()))
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
func waitForCreds(ac *Account) error {
	timeout := internal.Time().Add(30 * time.Second)
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
func createOrgAccessRole(c *iam.IAM, masterAccountID string) error {
	assumeRolePolicy := newAssumeRolePolicy(masterAccountID)
	role := iam.CreateRoleInput{
		AssumeRolePolicyDocument: aws.String(assumeRolePolicy),
		RoleName:                 aws.String("OrganizationAccountAccessRole"),
	}
	policy := iam.PutRolePolicyInput{
		PolicyDocument: aws.String(adminPolicy),
		PolicyName:     aws.String("AdministratorAccess"),
		RoleName:       role.RoleName,
	}
	// New credentials for a new account sometimes result in
	// InvalidClientTokenId error for the first few seconds.
	timeout := internal.Time().Add(10 * time.Second)
	for {
		_, err := c.CreateRole(&role)
		if err == nil {
			_, err = c.PutRolePolicy(&policy)
			return err
		}
		e, ok := err.(awserr.Error)
		if !ok || e.Code() != "InvalidClientTokenId" ||
			!internal.Time().Before(timeout) {
			return err
		}
		time.Sleep(time.Second)
	}
}
