package cmd

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/LuminalHQ/oktapus/internal"
	"github.com/LuminalHQ/oktapus/okta"
	"github.com/LuminalHQ/oktapus/op"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/arn"
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
		Usage:   "account-spec command ...",
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
		Okta. In this mode, account names are derived from Okta app labels. The
		account-spec can be used to filter apps by label or account ID. The
		following command lists all available AWS apps in Okta:

		  oktapus exec -okta "" true

		Due to Okta request rate limits, this command may take a long time to
		execute if there are hundreds of AWS apps in your Okta account (but who
		would ever have that many apps, right?).
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
	var credsOut []*credsOutput
	if cmd.OktaApps {
		if ctx.Sess == nil {
			if ctx.Sess, err = op.NewSession(nil); err != nil {
				return err
			}
		}
		credsOut, err = oktaCreds(ctx, args[0])
	} else {
		cmd := op.GetCmdInfo("creds").New().(*creds)
		cmd.Spec, args = args[0], args[1:]
		var out interface{}
		if out, err = ctx.Call(cmd); err == nil {
			credsOut = out.([]*credsOutput)
		}
	}
	if err != nil {
		return err
	}
	env := os.Environ()
	env = append(make([]string, 0, len(env)+5), env...)
	credsFail, cmdFail := 0, 0
	for _, cr := range credsOut {
		fmt.Fprintf(os.Stderr, "===> Account %s (%s)\n", cr.AccountID, cr.Name)
		if cr.Error != "" {
			fmt.Fprintf(os.Stderr, "===> ERROR: %s\n", cr.Error)
			credsFail++
			continue
		}
		awsEnv := []string{
			"AWS_ACCOUNT_ID=" + cr.AccountID,
			"AWS_ACCOUNT_NAME=" + cr.Name,
			"AWS_ACCESS_KEY_ID=" + cr.AccessKeyID,
			"AWS_SECRET_ACCESS_KEY=" + cr.SecretAccessKey,
			"AWS_SESSION_TOKEN=" + cr.SessionToken,
		}
		c := exec.Cmd{
			Path:   path,
			Args:   args,
			Env:    append(env, awsEnv...),
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

// oktaCreds returns credentials for all AWS apps in Okta.
func oktaCreds(ctx *op.Ctx, spec string) ([]*credsOutput, error) {
	c := ctx.Okta()
	apps, err := c.AppLinks()
	if err != nil {
		return nil, err
	}
	links := make([]*okta.AppLink, 0, len(apps))
	for _, app := range apps {
		if app.AppName == "amazon_aws" {
			links = append(links, app)
		}
	}
	if len(links) == 0 {
		return nil, errors.New("no aws apps found")
	}
	creds := make([]*credsOutput, len(links))
	internal.GoForEach(len(links), 40, func(i int) error {
		timeout := internal.Time().Add(time.Minute)
	retry:
		out, err := getAppCreds(c, links[i], ctx.Sess)
		if err == nil {
			creds[i] = out
			return nil
		}
		if err == okta.ErrRateLimit && internal.Time().Before(timeout) {
			// Limit is 40 requests per 10 seconds
			// https://support.okta.com/help/Documentation/Knowledge_Article/API-54325410
			internal.Sleep(15 * time.Second)
			goto retry
		}
		creds[i] = &credsOutput{
			Name:  links[i].Label,
			Error: explainError(err),
		}
		return nil
	})
	return filterCreds(creds, spec)
}

// getAppCreds returns credentials for an AWS app in Okta.
func getAppCreds(c *okta.Client, app *okta.AppLink, cp client.ConfigProvider) (*credsOutput, error) {
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

	// TODO: Improve GovCloud handling

	// Exchange SAML assertion for temporary security credentials
	cfg := aws.Config{Credentials: credentials.AnonymousCredentials}
	role, _ := arn.Parse(auth.Roles[0].Role)
	if role.Partition == endpoints.AwsUsGovPartitionID {
		cfg.EndpointResolver = endpoints.AwsUsGovPartition()
		cfg.Region = aws.String(endpoints.UsGovWest1RegionID)
	}
	stsc := sts.New(cp, &cfg)
	cr := auth.Creds(stsc.AssumeRoleWithSAML, auth.Roles[0])
	v, err := cr.Creds().Get()
	if err != nil {
		log.E("Failed to get credentials for %q: %v", app.Label, err)
		return nil, err
	}

	// Get account ID
	stsc.Config.Credentials = cr.Creds()
	ident, err := stsc.GetCallerIdentity(nil)
	if err != nil {
		log.E("Failed to get account ID for %q: %v", app.Label, err)
		return nil, err
	}
	return &credsOutput{
		AccountID:       aws.StringValue(ident.Account),
		Name:            app.Label,
		Expires:         expTime{cr.Expires()},
		AccessKeyID:     v.AccessKeyID,
		SecretAccessKey: v.SecretAccessKey,
		SessionToken:    v.SessionToken,
	}, nil
}

// filterCreds uses account filtering logic to filter credentials by account id
// or name.
func filterCreds(creds []*credsOutput, spec string) ([]*credsOutput, error) {
	idm := make(map[string]*credsOutput, len(creds))
	acs := make(op.Accounts, 0, len(creds))
	for _, cr := range creds {
		if other := idm[cr.AccountID]; other != nil {
			log.W("Credentials for %q and %q refer to the same AWS account (%s)",
				other.Name, cr.Name, cr.AccountID)
		} else {
			idm[cr.AccountID] = cr
			ac := op.NewAccount(cr.AccountID, cr.Name)
			// TODO: Get real control info?
			ac.Ctl = new(op.Ctl)
			acs = append(acs, ac)
		}
	}
	acs, err := op.ParseAccountSpec(spec, "").Filter(acs)
	if err != nil || len(acs) == 0 {
		return nil, err
	}
	creds = creds[:0]
	for _, ac := range acs.Sort() {
		creds = append(creds, idm[ac.ID])
	}
	return creds, nil
}
