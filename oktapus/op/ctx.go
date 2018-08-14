package op

import (
	"crypto/hmac"
	"crypto/sha512"
	"encoding/hex"
	"os"
	"os/user"
	"path"
	"path/filepath"
	"reflect"
	"strconv"

	"github.com/LuminalHQ/cloudcover/oktapus/account"
	"github.com/LuminalHQ/cloudcover/oktapus/creds"
	"github.com/LuminalHQ/cloudcover/x/arn"
	"github.com/LuminalHQ/cloudcover/x/cli"
	"github.com/LuminalHQ/cloudcover/x/fast"
	"github.com/LuminalHQ/cloudcover/x/iamx"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/external"
	orgs "github.com/aws/aws-sdk-go-v2/service/organizations"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/pkg/errors"
)

// Paths for managed IAM users and roles.
const (
	IAMPath    = "/oktapus/"
	IAMTmpPath = IAMPath + "tmp/"
)

// cmd is a command that runs with the given context.
type cmd interface {
	cli.Cmd
	Run(ctx *Ctx) (interface{}, error)
}

// Run executes the specified command with an initialized context.
func Run(c cmd) (interface{}, error) {
	ctx := NewCtx()
	if err := ctx.Init(nil); err != nil {
		return nil, err
	}
	return c.Run(ctx)
}

// Ctx provides global configuration information and account access.
type Ctx struct {
	// Oktapus environment config
	AliasFile  string `env:"OKTAPUS_ALIAS_FILE"`
	Profile    string `env:"OKTAPUS_AWS_PROFILE"`
	MasterRole string `env:"OKTAPUS_MASTER_ROLE"`
	CommonRole string `env:"OKTAPUS_COMMON_ROLE"`
	NoDaemon   bool   `env:"OKTAPUS_NO_DAEMON"`

	// Okta environment config using same variables as:
	// https://github.com/oktadeveloper/okta-aws-cli-assume-role/
	OktaHost    string `env:"OKTA_ORG"`
	OktaSID     string `env:"OKTA_SID"`
	OktaUser    string `env:"OKTA_USERNAME"`
	OktaAWSApp  string `env:"OKTA_AWS_APP_URL"`
	OktaAWSRole string `env:"OKTA_AWS_ROLE_TO_ASSUME"`

	// AWS environment config
	EnvCfg external.EnvConfig

	cfg   aws.Config
	proxy creds.Proxy
	dir   account.Directory
	creds map[string]*creds.Provider
	acs   map[string]*Account
}

// NewCtx returns a new context configured from the environment variables.
func NewCtx() *Ctx {
	c := new(Ctx)
	if u, err := user.Current(); err == nil && u.HomeDir != "" {
		c.AliasFile = filepath.Join(u.HomeDir, ".aws", "oktapus-accounts")
	}
	if err := setEnvFields(c); err != nil {
		panic(err)
	}
	c.EnvCfg, _ = external.NewEnvConfig()
	return c
}

// Init initializes the context before first use. If cfg is nil, the client
// configuration is loaded from Ctx state and shared AWS config files.
func (c *Ctx) Init(cfg *aws.Config) error {
	if err := c.initClients(cfg); err != nil {
		return err
	}
	err := fast.Call(c.proxy.Init, c.dir.Init)
	if err != nil && !account.IsErrorNoOrg(err) {
		return errors.WithStack(err)
	}
	if c.CommonRole == "" {
		c.CommonRole = IAMPath + c.proxy.SessName
	}
	master := c.dir.Org.MasterID
	if master != "" && c.proxy.Ident.Account != master {
		if c.MasterRole == "" {
			c.MasterRole = IAMPath + "OktapusOrganizationsProxy"
		}
		creds.Set(c.dir.Client.Client, c.proxy.Provider(&sts.AssumeRoleInput{
			ExternalId:      c.MasterExternalID(),
			RoleArn:         arn.String(c.proxy.Role(master, c.MasterRole)),
			RoleSessionName: aws.String(c.proxy.SessName),
		}))
	}
	return nil
}

// Cfg returns the active AWS configuration.
func (c *Ctx) Cfg() aws.Config { return c.cfg }

// Ident returns the identity of the gateway credentials.
func (c *Ctx) Ident() creds.Ident { return c.proxy.Ident }

// Org returns organization info.
func (c *Ctx) Org() account.Org { return c.dir.Org }

// Refresh updates the list of known accounts from the alias file and AWS
// Organizations API.
func (c *Ctx) Refresh() error {
	// TODO: Handle exec mode
	c.acs = make(map[string]*Account)
	if c.AliasFile != "" {
		m, err := account.LoadAliases(c.AliasFile, c.proxy.Ident.Partition())
		if err != nil && !os.IsNotExist(err) {
			return errors.WithStack(err)
		}
		acs := make(Accounts, 0, len(m))
		for id, name := range m {
			acs = append(acs, NewAccount(id, name))
		}
		c.Register(acs)
	}
	if err := c.dir.Refresh(); err != nil && !account.IsErrorNoOrg(err) {
		return errors.WithStack(err)
	}
	acs := make(Accounts, 0, len(c.dir.Accounts))
	for _, ac := range c.dir.Accounts {
		acs = append(acs, NewAccount(ac.ID, ac.Name))
	}
	c.Register(acs)
	return nil
}

// Register adds new accounts to the context and configures their IAM clients.
func (c *Ctx) Register(acs Accounts) Accounts {
	if c.acs == nil {
		if len(acs) == 0 {
			return acs
		}
		c.acs = make(map[string]*Account, len(acs))
	}
	for _, ac := range acs {
		if !account.IsID(ac.ID) {
			panic("op: invalid account id: " + ac.ID)
		}
		ac.IAM = iamx.New(&c.cfg)
		creds.Set(ac.IAM.Client, c.CredsProvider(ac.ID))
		c.acs[ac.ID] = ac
	}
	return acs
}

// Accounts returns all registered accounts sorted by name.
func (c *Ctx) Accounts() Accounts {
	if len(c.acs) == 0 {
		return nil
	}
	acs := make(Accounts, 0, len(c.acs))
	for _, ac := range c.acs {
		acs = append(acs, ac)
	}
	return acs.Sort()
}

// Match returns all accounts that match the spec.
func (c *Ctx) Match(spec string) (Accounts, error) {
	if c.acs == nil {
		if err := c.Refresh(); err != nil {
			return nil, err
		}
	}
	all := c.Accounts().LoadCtl(false)
	return ParseAccountSpec(spec, path.Base(c.CommonRole)).Filter(all)
}

// CredsProvider returns a credentials provider for the specified account ID.
func (c *Ctx) CredsProvider(accountID string) *creds.Provider {
	cp := c.creds[accountID]
	if cp != nil {
		return cp
	}
	cp = c.proxy.AssumeRole(c.proxy.Role(accountID, c.CommonRole), 0)

	// For the gateway account, try to assume the common role, but fall back
	// to original creds if that role does not exist.
	if accountID == c.proxy.Ident.Account {
		commonRole := cp
		var src *creds.Provider
		cp = creds.RenewableProvider(func() (aws.Credentials, error) {
			if src == nil {
				if commonRole.Ensure(-1) == nil {
					src = commonRole
					return commonRole.Creds()
				}
				src = c.cfg.Credentials.(*creds.Provider)
			}
			src.Ensure(-1)
			return src.Creds()
		})
	}
	if c.creds == nil {
		c.creds = make(map[string]*creds.Provider)
	}
	c.creds[accountID] = cp
	return cp
}

// MasterExternalID derives the external id for the master role.
func (c *Ctx) MasterExternalID() *string {
	org := &c.dir.Org
	if org.ID == "" {
		return nil
	}
	var buf [64]byte
	b := append(buf[:0], "oktapus:"...)
	b = append(b, org.MasterID...)
	b = append(b, ':')
	b = append(b, org.MasterEmail...)
	h := hmac.New(sha512.New512_256, []byte(org.ID))
	h.Write(b)
	b = h.Sum(b[:0])
	return aws.String(hex.EncodeToString(b))
}

// MarshalBinary implements encoding.BinaryMarshaler.
func (c *Ctx) MarshalBinary() ([]byte, error) {
	return nil, nil
}

// UnmarshalBinary implements encoding.BinaryUnmarshaler.
func (c *Ctx) UnmarshalBinary(b []byte) error {
	return nil
}

// initClients initializes AWS configuration and creates service clients.
func (c *Ctx) initClients(cfg *aws.Config) error {
	if cfg == nil {
		ext := external.Configs{
			external.WithSharedConfigProfile(c.Profile),
			c.EnvCfg,
			nil,
		}[:2]
		sc, err := external.LoadSharedConfigIgnoreNotExist(ext)
		if err != nil {
			return errors.WithStack(err)
		}
		ext = append(ext, sc)
		c.cfg, err = ext.ResolveAWSConfig(external.DefaultAWSConfigResolvers)
		if err != nil {
			return errors.WithStack(err)
		}
	} else {
		c.cfg = *cfg
	}
	// TODO: Okta creds
	c.cfg.Credentials = creds.StaticProvider(c.cfg.Credentials.Retrieve())
	c.proxy.Client = *sts.New(c.cfg)
	c.dir.Client = *orgs.New(c.cfg)
	return nil
}

// setEnvFields takes a struct pointer and sets field values from environment
// variables, with variable names obtained from the "env" field tag.
func setEnvFields(i interface{}) error {
	v := reflect.ValueOf(i).Elem()
	t := v.Type()
	for i := t.NumField() - 1; i >= 0; i-- {
		f := t.Field(i)
		key := f.Tag.Get("env")
		if key == "" {
			continue
		}
		val, ok := os.LookupEnv(key)
		if !ok {
			continue
		}
		switch f.Type.Kind() {
		case reflect.String:
			v.Field(i).Set(reflect.ValueOf(val))
		case reflect.Bool:
			b := val == ""
			if !b {
				var err error
				if b, err = strconv.ParseBool(val); err != nil {
					return errors.Wrapf(err, "invalid value for %q", key)
				}
			}
			v.Field(i).Set(reflect.ValueOf(b))
		default:
			panic("unsupported field type " + f.Type.String())
		}
	}
	return nil
}
