package op

import (
	"errors"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/LuminalHQ/cloudcover/oktapus/awsx"
	"github.com/LuminalHQ/cloudcover/oktapus/creds"
	"github.com/LuminalHQ/cloudcover/oktapus/daemon"
	"github.com/LuminalHQ/cloudcover/oktapus/internal"
	"github.com/LuminalHQ/cloudcover/oktapus/okta"
	"github.com/LuminalHQ/cloudcover/x/arn"
	"github.com/LuminalHQ/cloudcover/x/cli"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/external"
	"github.com/aws/aws-sdk-go-v2/service/sts"
)

var log = internal.Log

// IAMPath is a common path for managed IAM users and roles.
const IAMPath = "/oktapus/"

// TmpIAMPath is a path for temporary users and roles.
const TmpIAMPath = IAMPath + "tmp/"

// Environment variables that affect Ctx operation. Okta vars match those from
// github.com/oktadeveloper/okta-aws-cli-assume-role.
const (
	OktaHostEnv    = "OKTA_ORG"
	OktaSessEnv    = "OKTA_SID"
	OktaUserEnv    = "OKTA_USERNAME"
	OktaAWSAppEnv  = "OKTA_AWS_APP_URL"
	OktaAWSRoleEnv = "OKTA_AWS_ROLE_TO_ASSUME"

	AWSProfileEnv = "OKTAPUS_AWS_PROFILE"
	MasterRoleEnv = "OKTAPUS_MASTER_ROLE"
	CommonRoleEnv = "OKTAPUS_COMMON_ROLE"
	NoDaemonEnv   = "OKTAPUS_NO_DAEMON"
	AliasEnv      = "OKTAPUS_ALIAS_FILE"

	AccountIDEnv    = "AWS_ACCOUNT_ID"
	AccountNameEnv  = "AWS_ACCOUNT_NAME"
	AccessKeyIDEnv  = "AWS_ACCESS_KEY_ID"
	SecretKeyEnv    = "AWS_SECRET_ACCESS_KEY"
	SessionTokenEnv = "AWS_SESSION_TOKEN"
)

// Ctx provides global configuration information.
type Ctx struct {
	Env map[string]string
	All Accounts

	UseDaemon bool

	cfg  aws.Config
	okta *okta.Client
	gw   *awsx.Gateway
}

// NewCtx returns a new command execution context configured from environment
// variables.
func NewCtx() *Ctx {
	env := make(map[string]string)
	for _, kv := range os.Environ() {
		if len(kv) > 4 && (kv[:4] == "OKTA" || kv[:4] == "AWS_") {
			i := strings.IndexByte(kv, '=')
			env[kv[:i]] = kv[i+1:]
		}
	}
	ctx := &Ctx{Env: env, UseDaemon: true}
	if v, ok := ctx.Env[NoDaemonEnv]; ok {
		no, err := strconv.ParseBool(v)
		ctx.UseDaemon = err == nil && !no
	}
	return ctx
}

// UseOkta returns true if Okta is used for authentication.
func (ctx *Ctx) UseOkta() bool {
	return ctx.Env[OktaHostEnv] != ""
}

// InExec returns true if the process was started by the exec command. Only one
// account is accessible in this mode, with credentials provided in environment
// variables.
func (ctx *Ctx) InExec() bool {
	return !ctx.UseDaemon && awsx.IsAccountID(ctx.Env[AccountIDEnv])
}

// Cfg returns current AWS configuration.
func (ctx *Ctx) Cfg() *aws.Config {
	if ctx.cfg.EndpointResolver == nil {
		var err error
		if ctx.cfg, err = LoadConfig(); err != nil {
			log.F("Failed to load AWS config: %v", err)
		}
		if ctx.UseOkta() {
			ctx.cfg.Credentials = ctx.newOktaCreds(&ctx.cfg)
		}
	}
	return &ctx.cfg
}

// Okta returns an Okta client.
func (ctx *Ctx) Okta() *okta.Client {
	if ctx.okta != nil {
		return ctx.okta
	}
	host := ctx.Env[OktaHostEnv]
	if host == "" {
		log.F("Okta host not configured")
	}
	c := okta.NewClient(host)
	if sid := ctx.Env[OktaSessEnv]; sid != "" {
		if err := c.RefreshSession(sid); err != nil {
			log.F("Failed to refresh Okta session: %v", err)
		}
	} else {
		authn := newTermAuthn(ctx.Env[OktaUserEnv])
		if err := c.Authenticate(authn); err != nil {
			log.F("Okta authentication failed: %v", err)
		}
	}
	ctx.okta = c
	return c
}

// Gateway returns an AWS gateway client.
func (ctx *Ctx) Gateway() *awsx.Gateway {
	if ctx.gw != nil {
		return ctx.gw
	}
	gw := awsx.NewGateway(ctx.Cfg())
	gw.MasterRole = arn.Base + "role" + IAMPath + "OktapusOrganizationsProxy"
	if r := ctx.Env[MasterRoleEnv]; r != "" {
		gw.MasterRole = gw.MasterRole.WithPathName(r)
	}
	aliasFile, ok := ctx.Env[AliasEnv]
	if !ok {
		if u, err := user.Current(); err == nil && u.HomeDir != "" {
			aliasFile = filepath.Join(u.HomeDir, ".oktapus_accounts")
		}
	}
	if err := gw.Init(aliasFile); err != nil {
		log.F("Gateway initialization failed: %v", err)
	}
	if r := ctx.Env[CommonRoleEnv]; r != "" {
		gw.CommonRole = gw.CommonRole.WithPathName(r)
	} else {
		gw.CommonRole = gw.CommonRole.WithPath(IAMPath)
	}
	ctx.gw = gw
	return gw
}

// Accounts returns all accounts in the organization that match the spec.
func (ctx *Ctx) Accounts(spec string) (Accounts, error) {
	gw := ctx.Gateway()
	if ctx.All == nil {
		if ctx.InExec() {
			// TODO: Forward creds expiration time when supported
			ac := NewAccount(ctx.Env[AccountIDEnv], ctx.Env[AccountNameEnv])
			ac.Init(ctx.Cfg(), creds.StaticProvider(aws.Credentials{
				AccessKeyID:     ctx.Env[AccessKeyIDEnv],
				SecretAccessKey: ctx.Env[SecretKeyEnv],
				SessionToken:    ctx.Env[SessionTokenEnv],
			}, nil))
			ctx.All = Accounts{ac}
		} else {
			if err := gw.Refresh(); err != nil {
				log.E("Failed to list accounts")
				return nil, err
			}
			info := gw.Accounts()
			ctx.All = make(Accounts, len(info))
			for i, ac := range info {
				n := NewAccount(ac.ID, ac.DisplayName())
				n.Init(ctx.Cfg(), gw.CredsProvider(ac.ID))
				ctx.All[i] = n
			}
		}
	}
	ctx.All.RequireCtl()
	acs, err := ParseAccountSpec(spec, gw.CommonRole.Name()).Filter(ctx.All)
	return acs.Sort(), err
}

// CallableCmd is a command that can be called remotely.
type CallableCmd interface {
	cli.Cmd
	Call(ctx *Ctx) (interface{}, error)
}

// Call executes cmd locally or via a daemon process.
func (ctx *Ctx) Call(cmd CallableCmd) (interface{}, error) {
	if ctx.UseDaemon {
		return daemon.Call(ctx, cmd)
	}
	return cmd.Call(ctx)
}

// EnvMap returns an environment map for generating daemon signature.
func (ctx *Ctx) EnvMap() map[string]string {
	uid := ""
	if u, err := user.Current(); err == nil {
		uid = u.Uid
	}
	akid := ""
	if !ctx.UseOkta() {
		if cfg, err := LoadConfig(); err == nil {
			v, err := cfg.Credentials.Retrieve()
			if err == nil && v.SessionToken == "" {
				akid = v.AccessKeyID
			}
		}
	}
	keys := []string{
		OktaHostEnv,
		OktaSessEnv,
		OktaUserEnv,
		OktaAWSAppEnv,
		OktaAWSRoleEnv,
		MasterRoleEnv,
		CommonRoleEnv,
	}
	m := map[string]string{
		"VERSION":      internal.AppVersion,
		"UID":          uid,
		AccessKeyIDEnv: akid,
	}
	for _, k := range keys {
		m[k] = ctx.Env[k]
	}
	return m
}

// StartDaemon configures and starts a new daemon process.
func (ctx *Ctx) StartDaemon(c *exec.Cmd) error {
	if ctx.UseOkta() {
		s := ctx.Okta().Session()
		c.Env = append(c.Env, OktaSessEnv+"="+s.ID)
	}
	return c.Start()
}

// newOktaCreds returns a credentials provider that obtains a SAML assertion
// from Okta and exchanges it for temporary security credentials.
func (ctx *Ctx) newOktaCreds(cfg *aws.Config) *creds.Provider {
	c := sts.New(*cfg)
	creds.Set(c.Client, aws.AnonymousCredentials)
	awsAppLink := ctx.Env[OktaAWSAppEnv]
	return creds.RenewableProvider(func() (aws.Credentials, error) {
		var cr aws.Credentials
		k := ctx.Okta()
		if awsAppLink == "" {
			apps, err := k.AppLinks()
			if err != nil {
				return cr, err
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
				return cr, errors.New("AWS app not found in Okta")
			} else if multiple {
				log.W("Multiple AWS apps found in Okta, using %q", app.Label)
			}
			awsAppLink = app.LinkURL
		}
		auth, err := k.OpenAWS(awsAppLink, arn.ARN(ctx.Env[OktaAWSRoleEnv]))
		if err != nil {
			return cr, err
		}
		if len(auth.Roles) > 1 {
			log.W("Multiple AWS roles available, using %q", auth.Roles[0].Role)
		}
		var in sts.AssumeRoleWithSAMLInput
		auth.Use(auth.Roles[0], &in)
		out, err := c.AssumeRoleWithSAMLRequest(&in).Send()
		if err == nil {
			cr = creds.FromSTS(out.Credentials)
			cr.Source = "Okta"
		}
		return cr, err
	})
}

// LoadConfig loads current AWS config.
func LoadConfig() (aws.Config, error) {
	return external.LoadDefaultAWSConfig(
		external.WithSharedConfigProfile(os.Getenv(AWSProfileEnv)),
	)
}
