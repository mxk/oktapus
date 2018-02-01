package cmd

import (
	"errors"
	"os"
	"os/exec"
	"os/user"
	"sort"
	"strconv"

	"github.com/LuminalHQ/oktapus/awsgw"
	"github.com/LuminalHQ/oktapus/daemon"
	"github.com/LuminalHQ/oktapus/internal"
	"github.com/LuminalHQ/oktapus/okta"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/client"
	"github.com/aws/aws-sdk-go/aws/corehandlers"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/sts"
)

var log = internal.Log

// Ctx provides global configuration information.
type Ctx struct {
	OktaHost       string
	OktaSID        string
	OktaUser       string
	OktaAWSAppLink string
	AWSRoleARN     string
	NoDaemon       bool

	okta *okta.Client
	aws  *awsgw.Client
	all  Accounts
}

// NewCtx populates a new context from the environment variables.
func NewCtx() *Ctx {
	// Using same env vars as github.com/oktadeveloper/okta-aws-cli-assume-role
	ctx := &Ctx{
		OktaHost:       os.Getenv("OKTA_ORG"),
		OktaSID:        os.Getenv("OKTA_SID"),
		OktaUser:       os.Getenv("OKTA_USERNAME"),
		OktaAWSAppLink: os.Getenv("OKTA_AWS_APP_URL"),
		AWSRoleARN:     os.Getenv("OKTA_AWS_ROLE_TO_ASSUME"),
	}
	if v, ok := os.LookupEnv("OKTAPUS_NO_DAEMON"); ok {
		no, err := strconv.ParseBool(v)
		ctx.NoDaemon = err != nil || no
	}
	return ctx
}

// UseOkta returns true if Okta is used for authentication.
func (ctx *Ctx) UseOkta() bool {
	return ctx.OktaHost != ""
}

// Okta returns a client for Okta.
func (ctx *Ctx) Okta() *okta.Client {
	if ctx.okta != nil {
		return ctx.okta
	}
	if ctx.OktaHost == "" {
		log.F("Okta host not configured")
	}
	ctx.okta = okta.NewClient(ctx.OktaHost)
	if ctx.OktaSID != "" {
		if err := ctx.okta.RefreshSession(ctx.OktaSID); err != nil {
			log.F("Failed to refresh Okta session: %v", err)
		}
	} else {
		authn := newTermAuthn(ctx.OktaUser)
		if err := ctx.okta.Authenticate(authn); err != nil {
			log.F("Okta authentication failed: %v", err)
		}
	}
	return ctx.okta
}

// AWS returns an AWS gateway client.
func (ctx *Ctx) AWS() *awsgw.Client {
	if ctx.aws != nil {
		return ctx.aws
	}
	var cfg aws.Config
	if ctx.UseOkta() {
		// With Okta, all credentials must be explicit
		cfg.Credentials = credentials.NewCredentials(&credentials.ErrorProvider{
			Err:          errors.New("missing credentials"),
			ProviderName: "ErrorProvider",
		})
	}
	sess, err := newSession(&cfg)
	if err != nil {
		log.F("Failed to create AWS session: %v", err)
	}
	ctx.aws = awsgw.NewClient(sess)
	if ctx.UseOkta() {
		ctx.aws.MasterCreds = ctx.newOktaCreds(sess)
	}
	if err = ctx.aws.Connect(); err != nil {
		log.F("AWS connection failed: %v", err)
	}
	return ctx.aws
}

// Accounts returns all accounts in the organization that match the spec.
func (ctx *Ctx) Accounts(spec string) (Accounts, error) {
	c := ctx.AWS()
	if ctx.all == nil {
		if err := c.Refresh(); err != nil {
			return nil, err
		}
		info := c.Accounts()
		ctx.all = make(Accounts, len(info))
		for i, ac := range info {
			ctx.all[i] = &Account{Account: ac}
		}
	}
	ctx.all.RequireIAM(c).RequireCtl()
	acs, err := newAccountSpec(spec, c.CommonRole).Filter(ctx.all)
	sort.Sort(byName(acs))
	return acs, err
}

// Call executes cmd locally or via a daemon process.
func (ctx *Ctx) Call(cmd CallableCmd) (interface{}, error) {
	if ctx.NoDaemon {
		return cmd.Call(ctx)
	}
	return daemon.Call(ctx, cmd)
}

// EnvMap returns an environment map for generating daemon signature.
func (ctx *Ctx) EnvMap() map[string]string {
	uid := ""
	if u, err := user.Current(); err == nil {
		uid = u.Uid
	}
	akid := ""
	if !ctx.UseOkta() {
		if sess, err := newSession(nil); err == nil {
			v, err := sess.Config.Credentials.Get()
			if err == nil && v.SessionToken == "" {
				akid = v.AccessKeyID
			}
		}
	}
	return map[string]string{
		"VERSION":                 internal.AppVersion,
		"UID":                     uid,
		"AWS_ACCESS_KEY_ID":       akid,
		"OKTA_ORG":                ctx.OktaHost,
		"OKTA_SID":                ctx.OktaSID,
		"OKTA_USERNAME":           ctx.OktaUser,
		"OKTA_AWS_APP_URL":        ctx.OktaAWSAppLink,
		"OKTA_AWS_ROLE_TO_ASSUME": ctx.AWSRoleARN,
	}
}

// StartDaemon configures and starts a new daemon process.
func (ctx *Ctx) StartDaemon(c *exec.Cmd) error {
	if ctx.UseOkta() {
		s := ctx.Okta().Session()
		c.Env = append(c.Env, "OKTA_SID="+s.ID)
	}
	return c.Start()
}

// newOktaCreds returns a CredsProvider that obtains a SAML assertion from Okta
// and exchanges it for temporary security credentials.
func (ctx *Ctx) newOktaCreds(sess client.ConfigProvider) awsgw.CredsProvider {
	cfg := aws.Config{Credentials: credentials.AnonymousCredentials}
	c := awsgw.NewSAMLCreds(sts.New(sess, &cfg).AssumeRoleWithSAML, "", "", "")
	c.Renew = func(in *sts.AssumeRoleWithSAMLInput) error {
		c := ctx.Okta()
		if ctx.OktaAWSAppLink == "" {
			apps, err := c.AppLinks()
			if err != nil {
				return err
			}
			var app *okta.AppLink
			multiple := false
			for _, a := range apps {
				if a.AppName == "amazon_aws" {
					if app == nil {
						app = a
					} else {
						multiple = true
					}
				}
			}
			if app == nil {
				return errors.New("AWS app not found in Okta")
			} else if multiple {
				log.W("Multiple AWS apps found in Okta, using %q", app.Label)
			}
			ctx.OktaAWSAppLink = app.LinkURL
		}
		auth, err := c.OpenAWS(ctx.OktaAWSAppLink, ctx.AWSRoleARN)
		if err != nil {
			return err
		}
		if len(auth.Roles) > 1 {
			log.W("Multiple AWS roles available, using %q", auth.Roles[0].Role)
		}
		auth.Use(auth.Roles[0], in)
		return nil
	}
	return c
}

// newSession returns a new AWS session with the given config.
func newSession(cfg *aws.Config) (*session.Session, error) {
	sess, err := session.NewSession(cfg)
	if err == nil {
		// Remove useless handler that writes messages to stdout
		sess.Handlers.Send.Remove(corehandlers.ValidateReqSigHandler)
	}
	return sess, err
}
