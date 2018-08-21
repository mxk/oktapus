package op

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha512"
	"encoding/gob"
	"encoding/hex"
	"io"
	"io/ioutil"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"time"

	"github.com/LuminalHQ/cloudcover/oktapus/account"
	"github.com/LuminalHQ/cloudcover/oktapus/creds"
	"github.com/LuminalHQ/cloudcover/oktapus/daemon"
	"github.com/LuminalHQ/cloudcover/x/arn"
	"github.com/LuminalHQ/cloudcover/x/cli"
	"github.com/LuminalHQ/cloudcover/x/fast"
	"github.com/LuminalHQ/cloudcover/x/iamx"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/external"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/pkg/errors"
)

func init() {
	gob.Register((*GetCtx)(nil))
	gob.Register((*SavedCtx)(nil))
	gob.Register(Error(""))
}

// Paths for managed IAM users and roles.
const (
	IAMPath    = "/oktapus/"
	IAMTmpPath = IAMPath + "tmp/"
)

// cmd is a CLI command that requires a context.
type cmd interface {
	cli.Cmd
	Run(*Ctx) (interface{}, error)
}

// printCmd is a CLI command that can print its output.
type printCmd interface {
	cmd
	Print(interface{}) error
}

// Run executes the specified command with a local context.
func Run(c cmd) (interface{}, error) {
	ctx := EnvCtx()
	if err := ctx.Init(nil); err != nil {
		return nil, err
	}
	out, err := c.Run(ctx)
	if err2 := ctx.saveState(); err2 != nil && err == nil {
		err = err2
	}
	return out, err
}

// RunAndPrint executes the specified command with a local context and prints
// its output.
func RunAndPrint(c printCmd) error {
	out, err := Run(c)
	if err == nil {
		err = c.Print(out)
	}
	return err
}

// AuthMode is the context authentication mode.
type AuthMode int

const (
	Unknown AuthMode = iota // Context not initialized
	IAM                     // IAM user access key
	STS                     // STS session (single-account mode)
	Okta                    // Okta-federated IAM role
)

// Ver identifies the version of a type sent over a gob stream.
type Ver int

// Version returns v to satisfy a common interface.
func (v Ver) Version() Ver { return v }

// CtxVer identifies Ctx and SavedCtx struct versions. It should be incremented
// for any incompatible changes to force the daemon to restart.
const CtxVer = Ver(1)

// GetCtx is a daemon message requesting the context with the specified
// signature. The daemon either sends the matching *SavedCtx or closes the
// connection if the context was not found.
type GetCtx struct {
	Ver
	Sig string
}

// Error is an error type that can be encoded by gob.
type Error string

// Error implements error interface.
func (e Error) Error() string { return string(e) }

// Oktapus environment variables. Okta variables use same names as:
// https://github.com/oktadeveloper/okta-aws-cli-assume-role/
const (
	DaemonEnv     = "OKTAPUS_DAEMON"
	SecretFileEnv = "OKTAPUS_SECRET_FILE"
	AliasFileEnv  = "OKTAPUS_ALIAS_FILE"
	ProfileEnv    = "OKTAPUS_AWS_PROFILE"
	MasterRoleEnv = "OKTAPUS_MASTER_ROLE"
	CommonRoleEnv = "OKTAPUS_COMMON_ROLE"

	OktaHostEnv    = "OKTA_ORG"
	OktaSIDEnv     = "OKTA_SID"
	OktaUserEnv    = "OKTA_USERNAME"
	OktaAWSAppEnv  = "OKTA_AWS_APP_URL"
	OktaAWSRoleEnv = "OKTA_AWS_ROLE_TO_ASSUME"
)

// Ctx provides global config information and account access. A context can be
// local or non-local. Local contexts are allowed to access the file system,
// refresh accounts, communicate with the daemon, and perform other client
// functions. Non-local contexts, maintained by the daemon, are only allowed to
// make API calls to keep account credentials and control information current.
type Ctx struct {
	// Oktapus environment config
	Daemon     daemon.Addr `env:"OKTAPUS_DAEMON"`
	SecretFile string      `env:"OKTAPUS_SECRET_FILE"`
	AliasFile  string      `env:"OKTAPUS_ALIAS_FILE"`
	Profile    string      `env:"OKTAPUS_AWS_PROFILE"`
	MasterRole string      `env:"OKTAPUS_MASTER_ROLE"`
	CommonRole string      `env:"OKTAPUS_COMMON_ROLE"`

	// Okta environment config
	OktaHost    string `env:"OKTA_ORG"`
	OktaSID     string `env:"OKTA_SID"`
	OktaUser    string `env:"OKTA_USERNAME"`
	OktaAWSApp  string `env:"OKTA_AWS_APP_URL"`
	OktaAWSRole string `env:"OKTA_AWS_ROLE_TO_ASSUME"`

	// AWS environment config
	EnvCfg external.EnvConfig

	local  bool
	secret string
	mode   AuthMode
	cfg    aws.Config
	proxy  creds.Proxy
	dir    account.Directory
	creds  map[string]*creds.Provider
	acs    map[string]*Account
}

// NewCtx returns an empty local context.
func NewCtx() *Ctx { return &Ctx{local: true} }

// EnvCtx returns a local context populated from the environment variables.
func EnvCtx() *Ctx {
	awsDir := filepath.Dir(external.DefaultSharedConfigFiles[0])
	c := &Ctx{
		Daemon:     daemon.DefaultAddr,
		SecretFile: filepath.Join(awsDir, "oktapus.secret"),
		AliasFile:  filepath.Join(awsDir, "oktapus.accounts"),
		local:      true,
	}
	if err := setEnvFields(c); err != nil {
		panic(err)
	}
	c.EnvCfg, _ = external.NewEnvConfig()
	return c
}

// Init initializes a local context before first use. If cfg is nil, client
// config is loaded from context state and shared AWS config files.
func (c *Ctx) Init(cfg *aws.Config) error {
	if c.requireLocal(); c.mode != Unknown {
		panic("op: context already initialized")
	}
	if err := c.loadSecret(); err != nil {
		return err
	}
	if err := c.resolveCfg(cfg); err != nil {
		return err
	}
	if err, ok := c.restoreState(); err != nil {
		return err
	} else if !ok {
		c.newClients()
		err := fast.Call(c.proxy.Init, c.dir.Init)
		if err != nil && !account.IsErrorNoOrg(err) {
			return errors.Wrap(err, "client init failed")
		}
		c.setCommonRole()
	}
	c.setMasterCreds()
	return nil
}

// AuthMode returns the context authentication mode.
func (c *Ctx) AuthMode() AuthMode { return c.mode }

// Cfg returns the active AWS client config.
func (c *Ctx) Cfg() aws.Config { return c.cfg }

// Ident returns the identity of the client config credentials.
func (c *Ctx) Ident() creds.Ident { return c.proxy.Ident }

// Org returns organization info.
func (c *Ctx) Org() account.Org { return c.dir.Org }

// Save returns a serializable context representation.
func (c *Ctx) Save() *SavedCtx { return newSavedCtx(c) }

// Refresh updates the list of known accounts from the alias file and/or AWS
// Organizations API.
func (c *Ctx) Refresh() error {
	c.requireInit()
	c.requireLocal()
	// TODO: Handle STS single-account and EC2 instance role modes
	// TODO: Allow using "-" in alias file to remove org accounts?
	// TODO: Reuse accounts?
	c.acs = make(map[string]*Account)
	set := func(ac *Account, id, name string) *Account {
		ac.ID = id
		ac.Name = name
		return ac
	}
	if c.AliasFile != "" {
		m, err := account.LoadAliases(c.AliasFile, c.proxy.Ident.Partition())
		if err != nil {
			if !os.IsNotExist(err) {
				return errors.Wrap(err, "failed to load account aliases")
			}
		} else if len(m) > 0 {
			i, acs := 0, make([]Account, len(m))
			for id, name := range m {
				set(&acs[i], id, name)
				i++
			}
			c.Register(initAccounts(acs))
		}
	}
	if err := c.dir.Refresh(); err != nil && !account.IsErrorNoOrg(err) {
		return errors.Wrap(err, "failed to refresh accounts")
	}
	if len(c.dir.Accounts) > 0 {
		i, acs := 0, make([]Account, len(c.dir.Accounts))
		for _, info := range c.dir.Accounts {
			set(&acs[i], info.ID, info.Name).Set(OrgFlag)
			i++
		}
		c.Register(initAccounts(acs))
	}
	if c.acs[c.proxy.Ident.Account] == nil {
		// TODO: Add implicit proxy account
	}
	return nil
}

// Register adds new accounts to the context and configures their IAM clients.
func (c *Ctx) Register(acs Accounts) Accounts {
	c.requireInit()
	if c.acs == nil {
		if len(acs) == 0 {
			return acs
		}
		c.acs = make(map[string]*Account, len(acs))
		if c.creds == nil {
			c.creds = make(map[string]*creds.Provider, len(acs))
		}
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
	c.requireInit()
	if len(c.acs) == 0 {
		return nil
	}
	i, acs := 0, make(Accounts, len(c.acs))
	for _, ac := range c.acs {
		acs[i] = ac
		i++
	}
	return acs.SortByName()
}

// Match returns all accounts that match the spec.
func (c *Ctx) Match(spec string) (Accounts, error) {
	if c.acs == nil && c.local {
		if err := c.Refresh(); err != nil {
			return nil, err
		}
	}
	all := c.Accounts().LoadCtl(false)
	return ParseAccountSpec(spec, path.Base(c.CommonRole)).Filter(all)
}

// CredsProvider returns a credentials provider for the specified account ID.
func (c *Ctx) CredsProvider(accountID string) *creds.Provider {
	c.requireInit()
	cp := c.creds[accountID]
	if cp != nil {
		return cp
	}
	cp = c.proxy.AssumeRole(c.proxy.Role(accountID, c.CommonRole), 0)

	// For the gateway account, try to assume the common role first, but fall
	// back to original creds if that role does not exist.
	if accountID == c.proxy.Ident.Account {
		commonRole := cp
		proxyCreds := c.cfg.Credentials.(*creds.Provider)
		var src *creds.Provider
		cp = creds.RenewableProvider(func() (aws.Credentials, error) {
			if src == nil {
				if commonRole.Ensure(-1) == nil {
					src = commonRole
					return commonRole.Creds()
				}
				src = proxyCreds
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
	c.requireInit()
	org := &c.dir.Org
	if org.ID == "" {
		return nil
	}
	var buf [128]byte
	b := append(buf[:0], "oktapus:"...)
	b = append(b, org.MasterID...)
	b = append(b, ':')
	b = append(b, org.MasterEmail...)
	h := hmac.New(sha512.New512_256, []byte(org.ID))
	h.Write(b)
	b = h.Sum(b[:0])
	return aws.String(hex.EncodeToString(b))
}

// requireInit panics if the context was not initialized.
func (c *Ctx) requireInit() {
	if c.mode == Unknown {
		panic("op: context not initialized")
	}
}

// requireLocal panics if the context is non-local.
func (c *Ctx) requireLocal() {
	if !c.local {
		panic("op: illegal operation on non-local context")
	}
}

// loadSecret loads a secret string from a local file. If the file does not
// exist, a new secret is generated and saved. The secret is used for generating
// context signature to isolate user contexts on a shared daemon.
func (c *Ctx) loadSecret() error {
	if c.secret = ""; c.SecretFile == "" {
		return nil
	}
	b, err := ioutil.ReadFile(c.SecretFile)
	b = bytes.TrimSpace(b)
	if len(b) == 0 && (err == nil || os.IsNotExist(err)) {
		if err == nil {
			os.Remove(c.SecretFile) // Ensure correct permissions on new file
		}
		err = os.MkdirAll(filepath.Dir(c.SecretFile), os.ModePerm)
		if err == nil {
			b = make([]byte, 44, 45) // 260 bits of entropy
			copy(b, fast.RandID(len(b)))
			err = ioutil.WriteFile(c.SecretFile, append(b, '\n'), 0400)
		}
	}
	if err == nil {
		c.secret = string(b)
	}
	return errors.Wrap(err, "failed to get client secret")
}

// resolveCfg applies the specified client config or resolves a new one from
// context state and, if the context is local, shared AWS config files.
func (c *Ctx) resolveCfg(cfg *aws.Config) error {
	c.mode = Unknown
	if cfg == nil {
		ext := external.Configs{
			external.WithSharedConfigProfile(c.Profile),
			c.EnvCfg,
			nil,
		}[:2]
		if c.local {
			sc, err := external.LoadSharedConfigIgnoreNotExist(ext)
			if err != nil {
				return errors.Wrap(err, "failed to load shared config")
			}
			ext = append(ext, sc)
		}
		tmp, err := ext.ResolveAWSConfig(external.DefaultAWSConfigResolvers)
		if err != nil {
			return errors.Wrap(err, "failed to resolve client config")
		}
		c.cfg = tmp
	} else if cfg != &c.cfg {
		c.cfg = *cfg
	}
	if c.OktaHost == "" {
		cp := creds.WrapProvider(c.cfg.Credentials)
		c.cfg.Credentials = cp
		cr, err := cp.Creds()
		if err == nil && iamx.Is(cr.AccessKeyID, iamx.UserKey) {
			c.mode = IAM
		} else {
			// TODO: How to handle EC2 instance role creds?
			c.mode = STS
		}
	} else {
		// TODO: Okta creds; non-interactive if non-local
		c.mode = Okta
		panic("op: okta mode not implemented")
	}
	return nil
}

// newClients creates new AWS service clients.
func (c *Ctx) newClients() {
	c.proxy.Client = creds.NewClient(&c.cfg)
	c.dir.Client = account.NewClient(&c.cfg)
}

// restoreState attempts to restore context state from the daemon.
func (c *Ctx) restoreState() (error, bool) {
	sig := c.sig()
	if c.Daemon == "" || sig == "" {
		return nil, false
	}
	out, err := c.Daemon.Send(&GetCtx{CtxVer, sig})
	if err == nil {
		sc := out.(*SavedCtx)
		if sc.Sig != sig {
			panic("op: context signature mismatch")
		}
		sc.restore(c)
		return nil, true
	}
	if err == io.EOF || daemon.IsNotRunning(err) {
		err = nil
	}
	return errors.Wrap(err, "failed to restore state from daemon"), false
}

// saveState sends context state to the daemon. The daemon is started if not
// already running.
func (c *Ctx) saveState() error {
	if c.Daemon == "" || !c.local {
		return nil
	}
	sc := c.Save()
	if sc == nil {
		return nil
	}
	_, err := c.Daemon.Send(sc)
	if daemon.IsNotRunning(err) {
		if err = c.Daemon.Start(nil); err != nil {
			return errors.Wrapf(err, "failed to start daemon")
		}
		_, err = c.Daemon.Send(sc)
	}
	if err == io.EOF {
		err = nil
	}
	return errors.Wrap(err, "failed to save state to daemon")
}

// sig returns a hash of context config and client credentials. Two contexts
// with identical signatures have access to the same accounts.
func (c *Ctx) sig() string {
	c.requireInit()
	if c.secret == "" {
		return ""
	}
	sig := map[string]string{
		"SECRET":     c.secret,
		AliasFileEnv: c.AliasFile,
	}
	switch c.mode {
	case IAM:
		cr, _ := c.cfg.Credentials.(*creds.Provider).Creds()
		sig[external.AWSAccessKeyIDEnvVar] = cr.AccessKeyID
		sig[external.AWSSecreteAccessKeyEnvVar] = cr.SecretAccessKey
	case Okta:
		sig[OktaHostEnv] = c.OktaHost
		sig[OktaUserEnv] = c.OktaUser
		sig[OktaAWSAppEnv] = c.OktaAWSApp
		sig[OktaAWSRoleEnv] = c.OktaAWSRole
	default:
		// TODO: How to handle EC2 instance role?
		return ""
	}
	i, keys := 0, make([]string, len(sig))
	for k := range sig {
		keys[i] = k
		i++
	}
	sort.Strings(keys)
	var buf [2048]byte
	b := buf[:0]
	for _, k := range keys {
		b = append(b, k...)
		b = append(b, '=')
		b = append(b, sig[k]...)
		b = append(b, '\n')
	}
	sum := sha512.Sum512_256(b)
	return hex.EncodeToString(sum[:])
}

// setCommonRole sets the default common role if one was not specified.
func (c *Ctx) setCommonRole() {
	if c.CommonRole == "" {
		c.CommonRole = IAMPath + c.proxy.SessName
	}
}

// setMasterCreds updates account directory credentials if the current account
// is not the organization master.
func (c *Ctx) setMasterCreds() {
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
}

type savedCreds struct {
	Account string
	Creds   aws.Credentials
	Err     error
}

// saveCreds returns serializable copies of all credentials that are either
// valid until time t or have an unexpired error.
func (c *Ctx) saveCreds(t time.Time) []savedCreds {
	if len(c.creds) == 0 {
		return nil
	}
	i, crs := 0, make([]savedCreds, len(c.creds))
	for id, cp := range c.creds {
		sc := &crs[i]
		if sc.Creds, sc.Err = cp.Creds(); sc.Err != nil {
			sc.Err = Error(sc.Err.Error())
		} else if !creds.ValidUntil(&sc.Creds, t) {
			continue
		}
		sc.Account = id
		i++
	}
	if i == 0 {
		crs = nil
	}
	return crs[:i]
}

// saveAccounts returns serializable copies of all accounts.
func (c *Ctx) saveAccounts() []Account {
	if len(c.acs) == 0 {
		return nil
	}
	i, acs := 0, make([]Account, len(c.acs))
	for _, src := range c.acs {
		ac := &acs[i]
		ac.Flags = src.Flags
		ac.ID = src.ID
		ac.Name = src.Name
		ac.Ctl = src.Ctl
		i++
	}
	return acs
}

// TODO: Save/restore c.cfg creds in Okta mode

// SavedCtx is a serializable context representation.
type SavedCtx struct {
	Ver
	Ctx           Ctx
	Sig           string
	Secret        string
	ProxyIdent    creds.Ident
	ProxySessName string
	DirOrg        account.Org
	Creds         []savedCreds
	Accounts      []Account
}

// newSavedCtx returns a saved copy of c or nil if c cannot be saved.
func newSavedCtx(c *Ctx) *SavedCtx {
	sig := c.sig()
	if sig == "" {
		return nil
	}
	sc := &SavedCtx{
		Ver:           CtxVer,
		Ctx:           *c,
		Sig:           sig,
		Secret:        c.secret,
		ProxyIdent:    c.proxy.Ident,
		ProxySessName: c.proxy.SessName,
		DirOrg:        c.dir.Org,
		Creds:         c.saveCreds(fast.Time().Add(5 * time.Minute)),
		Accounts:      c.saveAccounts(),
	}
	c = &sc.Ctx
	envFromCfg(&c.EnvCfg, &c.cfg)
	c.local = false
	c.mode = Unknown
	c.creds = nil
	c.acs = nil
	return sc
}

// Restore creates a new non-local context from saved state.
func (sc *SavedCtx) Restore() (*Ctx, error) {
	c := sc.Ctx
	c.local = false
	c.secret = sc.Secret
	if err := c.resolveCfg(nil); err != nil {
		return nil, err
	}
	sc.restore(&c)
	c.setMasterCreds()
	return &c, nil
}

// restore restores saved state into an existing context. There are two modes of
// operation. The first is restoring a non-local context on the daemon, which
// restores all exported Ctx fields. The second is restoring into a new local
// context, which should keep all existing values for exported fields. It does
// not make sense for a cached context to override local environment variables,
// but this means that the cached accounts and creds may no longer be 100%
// correct for the current config.
func (sc *SavedCtx) restore(c *Ctx) {
	c.proxy.Ident = sc.ProxyIdent
	c.proxy.SessName = sc.ProxySessName
	c.dir.Org = sc.DirOrg
	c.newClients()
	c.setCommonRole()

	// Common role is not part of context signature because it does not affect
	// what the user has access to in general, but it does change the current
	// identity. It's fine to restore cached account information that the
	// current common role may not have access to, but cached creds can only be
	// restored for the same role. Account CredsFlag is not modified because any
	// command that requires explicit credentials should call EnsureCreds first.
	if len(sc.Accounts) > 0 {
		c.Register(initAccounts(sc.Accounts))
	}
	if c.CommonRole == sc.Ctx.CommonRole {
		for i := range sc.Creds {
			cr := &sc.Creds[i]
			c.CredsProvider(cr.Account).Store(cr.Creds, cr.Err)
		}
	}
}

// setEnvFields populates struct pointer field values from environment
// variables, with variable names obtained from the "env" field tag.
func setEnvFields(i interface{}) error {
	v := reflect.ValueOf(i).Elem()
	t := v.Type()
	stringType := reflect.TypeOf("")
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
			src := reflect.ValueOf(val)
			if f.Type != stringType {
				src = src.Convert(f.Type)
			}
			v.Field(i).Set(src)
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

// envFromCfg updates AWS environment config with relevant values extracted from
// a client config.
func envFromCfg(env *external.EnvConfig, cfg *aws.Config) {
	if cfg.Credentials != nil {
		if cr, err := creds.WrapProvider(cfg.Credentials).Creds(); err == nil {
			env.Credentials = cr
		}
	}
	if res, ok := cfg.EndpointResolver.(aws.ResolveWithEndpoint); ok {
		env.CredentialsEndpoint = res.URL
	}
	if cfg.Region != "" {
		env.Region = cfg.Region
	}
}
