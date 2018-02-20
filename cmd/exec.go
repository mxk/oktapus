package cmd

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/LuminalHQ/oktapus/awsx"
	"github.com/LuminalHQ/oktapus/internal"
	"github.com/LuminalHQ/oktapus/okta"
	"github.com/LuminalHQ/oktapus/op"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/client"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/endpoints"
	"github.com/aws/aws-sdk-go/service/sts"
)

// TODO: Parallel execution (close stdin, capture stdout/stderr)?
// TODO: Need to pass partition/region to command?

func init() {
	op.Register(&op.CmdInfo{
		Names:   []string{"exec"},
		Summary: "Run external command for multiple accounts",
		Usage:   "[-okta] account-spec command ...",
		MinArgs: 2,
		New:     func() op.Cmd { return &execCmd{Name: "exec"} },
	})
}

type execCmd struct {
	Name
	OktaApps bool
}

func (cmd *execCmd) Help(w *bufio.Writer) {
	op.WriteHelp(w, `
		Run external command for one or more accounts using temporary security
		credentials.

		The specified command is executed for each account matched by
		account-spec. Account information and credentials are passed in
		environment variables. The following command executes AWS CLI, which
		reports caller identity information for all accessible accounts:

		  oktapus exec "" aws sts get-caller-identity

		The following environment variables are set for each command execution:

		  AWS_ACCOUNT_ID
		  AWS_ACCOUNT_NAME
		  AWS_ACCESS_KEY_ID
		  AWS_SECRET_ACCESS_KEY
		  AWS_SESSION_TOKEN

		Account ID and name are non-standard variables that provide information
		about the current account. The remaining variables are the same ones
		used by AWS CLI and SDKs.

		The -okta option can be used to run the command for each AWS app in
		Okta. In this mode, account names are derived from IAM aliases, with
		Okta app labels used as fallback. The account-spec can be used to filter
		apps by label or account ID. The following command lists all available
		AWS apps in Okta:

		  oktapus exec -okta "" true

		Due to Okta request rate limits, this command may take a long time to
		execute if there are hundreds of AWS apps in your Okta account (but who
		would ever have that many apps, right?).

		Oktapus can execute itself with the exec command. This mostly needed to
		configure initial account access. In this mode, the gateway function is
		disabled and the given command operates on just one account, so the
		account-spec should be empty. For example:

		  oktapus exec -okta "" oktapus authz -principal ... "" user@example.com
	`)
	op.AccountSpecHelp(w)
}

func (cmd *execCmd) FlagCfg(fs *flag.FlagSet) {
	fs.BoolVar(&cmd.OktaApps, "okta", false,
		"Execute command for each AWS app in Okta")
}

func (cmd *execCmd) Run(ctx *op.Ctx, args []string) error {
	path, err := exec.LookPath(args[1])
	if err != nil {
		return err
	}
	spec, args := args[0], args[1:]
	var credsOut []*credsOutput
	if cmd.OktaApps {
		if ctx.Sess == nil {
			if ctx.Sess, err = op.NewSession(nil); err != nil {
				return err
			}
		}
		if ctx.All, err = oktaAccounts(ctx, spec); err != nil {
			return err
		}
		credsOut = listCreds(ctx.All, false)
	} else {
		cmd := op.GetCmdInfo("creds").New().(*creds)
		cmd.Spec = spec
		var out interface{}
		if out, err = ctx.Call(cmd); err != nil {
			return err
		}
		credsOut = out.([]*credsOutput)
	}
	env := execEnv()
	credsFail, cmdFail := 0, 0
	for i, cr := range credsOut {
		name := cr.Name
		if cmd.OktaApps {
			name += ": " + ctx.All[i].Desc
		}
		fmt.Fprintf(os.Stderr, "===> Account %s (%s)\n", cr.AccountID, name)
		if cr.Error != "" {
			fmt.Fprintf(os.Stderr, "===> ERROR: %s\n", cr.Error)
			credsFail++
			continue
		}
		c := exec.Cmd{
			Path:   path,
			Args:   args,
			Env:    mergeEnv(env, cr),
			Stdin:  os.Stdin,
			Stdout: os.Stdout,
			Stderr: os.Stderr,
		}
		if err := c.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "===> ERROR: %v\n", err)
			if cmdFail++; cmdFail == 1 && internal.ExitCode(err) == 2 {
				return fmt.Errorf("abort due to command usage error")
			}
		}
	}
	if n := credsFail + cmdFail; n != 0 {
		s := ""
		if n > 1 {
			s = "s"
		}
		return fmt.Errorf("%d command%s failed (%d due to invalid credentials)",
			n, s, credsFail)
	}
	return nil
}

// execEnv returns environment variables used for all command invocations.
func execEnv() []string {
	all := os.Environ()
	env := make([]string, 0, len(all)+8)
	for _, e := range all {
		if !strings.HasPrefix(e, "OKTA_") {
			env = append(env, e)
		}
	}
	env = append(env, op.NoDaemonEnv+"=1")
	return env
}

// mergeEnv returns command-specific environment configuration.
func mergeEnv(common []string, cr *credsOutput) []string {
	return append(common,
		op.AccountIDEnv+"="+cr.AccountID,
		op.AccountNameEnv+"="+cr.Name,
		op.AccessKeyIDEnv+"="+cr.AccessKeyID,
		op.SecretKeyEnv+"="+cr.SecretAccessKey,
		op.SessionTokenEnv+"="+cr.SessionToken,
	)
}

// oktaAccounts returns Accounts for all AWS apps in Okta that match the spec.
func oktaAccounts(ctx *op.Ctx, spec string) (op.Accounts, error) {
	c := ctx.Okta()
	apps, err := c.AppLinks()
	if err != nil {
		return nil, err
	}
	want := ctx.Env[op.OktaAWSAppEnv]
	links := make([]*okta.AppLink, 0, len(apps))
	for _, app := range apps {
		if app.AppName == "amazon_aws" && (want == "" || want == app.LinkURL) {
			links = append(links, app)
		}
	}
	if len(links) == 0 {
		return nil, errors.New("no aws apps found")
	}

	// Open all AWS apps
	all := make(op.Accounts, len(links))
	internal.GoForEach(len(links), 40, func(i int) error {
		timeout := internal.Time().Add(time.Minute)
		for {
			out, err := getAccount(c, links[i], ctx.Sess)
			if err == nil || err != okta.ErrRateLimit {
				all[i] = out
				return nil
			}
			if !internal.Time().Before(timeout) {
				break
			}
			// Limit is 40 requests per 10 seconds
			// https://support.okta.com/help/Documentation/Knowledge_Article/API-54325410
			internal.Sleep(12 * time.Second)
		}
		log.E("Timeout while waiting for AWS app %q: %v", links[i].Label, err)
		return nil
	})

	// Remove duplicate and invalid accounts
	ids := make(map[string]int, len(all))
	acs := all[:0]
	for i, ac := range all {
		if ac == nil {
			continue
		}
		if idx, dup := ids[ac.ID]; dup {
			log.W("Apps %q and %q refer to the same account %s (%s)",
				links[idx].Label, links[i].Label, ac.ID, ac.Name)
			continue
		}
		ids[ac.ID] = i
		acs = append(acs, ac)
	}

	// Apply account-spec
	acs, err = op.ParseAccountSpec(spec, "").Filter(acs)
	return acs.Sort(), err
}

// getAccount returns a new Account for an AWS app in Okta.
func getAccount(c *okta.Client, app *okta.AppLink, cp client.ConfigProvider) (*op.Account, error) {
	auth, err := c.OpenAWS(app.LinkURL, "")
	if err != nil {
		if err != okta.ErrRateLimit {
			log.E("Failed to open AWS app %q: %v", app.Label, err)
		}
		return nil, err
	}
	if len(auth.Roles) > 1 {
		log.W("Multiple AWS roles available for %q, using %q",
			app.Label, auth.Roles[0].Role)
	}
	r := auth.Roles[0]

	// Exchange SAML assertion for temporary security credentials
	cfg := aws.Config{Credentials: credentials.AnonymousCredentials}
	if r.Role.Partition() == endpoints.AwsUsGovPartitionID {
		cp = awsx.GovCloudConfigProvider{ConfigProvider: cp}
	}
	stsc := sts.New(cp, &cfg)
	cr := auth.Creds(stsc.AssumeRoleWithSAML, r)
	if _, err = cr.Creds().Get(); err != nil {
		log.E("Failed to get credentials for %q: %v", app.Label, err)
		return nil, err
	}

	// Create account
	ac := op.NewAccount(r.Role.Account(), app.Label)
	ac.Ctl = &op.Ctl{Desc: app.Label}
	ac.Init(cp, cr)
	out, err := ac.IAM().ListAccountAliases(nil)
	if len(out.AccountAliases) > 0 {
		if name := aws.StringValue(out.AccountAliases[0]); name != "" {
			ac.Name = name
		}
	} else if err != nil {
		log.W("Failed to get account alias %q: %v", app.Label, err)
	}
	return ac, nil
}
