package awsx

import (
	"crypto/hmac"
	"crypto/sha512"
	"encoding/hex"
	"errors"
	"os"

	"github.com/LuminalHQ/cloudcover/oktapus/account"
	"github.com/LuminalHQ/cloudcover/oktapus/creds"
	"github.com/LuminalHQ/cloudcover/x/arn"
	"github.com/LuminalHQ/cloudcover/x/fast"
	"github.com/aws/aws-sdk-go-v2/aws"
	orgs "github.com/aws/aws-sdk-go-v2/service/organizations"
	"github.com/aws/aws-sdk-go-v2/service/sts"
)

// Gateway provides access to multiple AWS accounts via one gateway account. It
// is not safe to use the client concurrently from multiple goroutines.
type Gateway struct {
	MasterRole arn.ARN // Master account role with ListAccounts permission
	CommonRole arn.ARN // Role to assume when accessing other accounts

	proxy *creds.Proxy
	dir   *account.Directory
	creds map[string]*creds.Provider

	aliasFile string
}

// NewGateway creates a new AWS gateway client. The client is not usable until
// Init() is called.
func NewGateway(cfg *aws.Config) *Gateway {
	return &Gateway{
		proxy: creds.NewProxy(cfg),
		dir:   account.NewDirectory(cfg),
	}
}

// Init initializes client identity and organization information.
func (gw *Gateway) Init(aliasFile string) error {
	gw.aliasFile = aliasFile
	err := fast.Call(
		func() error { return gw.proxy.Init() },
		func() error { return gw.dir.Init() },
	)
	if err == nil && !gw.IsMaster() {
		if gw.MasterRole == "" {
			return errors.New("awsx: gateway master role not set")
		}
		org := gw.dir.Org()
		gw.MasterRole = arn.New(org.ARN.Partition(), "iam", "", org.MasterID,
			"role", gw.MasterRole.Path(), gw.MasterRole.Name())
		cfg := gw.proxy.Client.Config
		cfg.Credentials = gw.proxy.Provider(&sts.AssumeRoleInput{
			RoleArn:         arn.String(gw.MasterRole),
			RoleSessionName: aws.String(gw.proxy.SessName),
			ExternalId:      aws.String(ProxyExternalID(&org)),
		})
		gw.dir = account.NewDirectory(&cfg)
		err = gw.dir.Init()
	}
	if gw.proxy.SessName != "" {
		gw.CommonRole = arn.New(gw.proxy.Ident.Partition(), "iam", "", "",
			"role/", gw.proxy.SessName)
	}
	if err == account.ErrNoOrg {
		err = nil
	}
	return err
}

// Ident returns the identity of the gateway credentials.
func (gw *Gateway) Ident() creds.Ident { return gw.proxy.Ident }

// Org returns information about the organization.
func (gw *Gateway) Org() account.Org { return gw.dir.Org() }

// IsMaster returns true if the gateway account is organization master.
func (gw *Gateway) IsMaster() bool {
	master := gw.dir.Org().MasterID
	return gw.proxy.Ident.Account == master && master != ""
}

// Refresh updates information about all accounts in the organization.
func (gw *Gateway) Refresh() error {
	if gw.aliasFile != "" {
		err := gw.dir.LoadAliases(gw.aliasFile)
		if err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	err := gw.dir.Refresh()
	if err == account.ErrNoOrg || err == account.ErrNotMaster {
		err = nil
	}
	return err
}

// Accounts returns information about all known accounts.
func (gw *Gateway) Accounts() []*account.Info {
	return gw.dir.Accounts()
}

// Account returns information for one account ID.
func (gw *Gateway) Account(id string) *account.Info {
	return gw.dir.Account(id)
}

// AddAccount adds a newly created account to the directory.
func (gw *Gateway) AddAccount(ac *orgs.Account) *account.Info {
	info := gw.dir.SetAlias(aws.StringValue(ac.Id), "")
	info.Set(ac)
	return info
}

// CredsProvider returns a credentials provider for the specified account ID.
func (gw *Gateway) CredsProvider(accountID string) *creds.Provider {
	cp := gw.creds[accountID]
	if cp == nil {
		cp = gw.AssumeRole(gw.CommonRole.WithAccount(accountID))
		if gw.creds == nil {
			gw.creds = make(map[string]*creds.Provider)
		}
		gw.creds[accountID] = cp
	}
	return cp
}

// AssumeRole returns new credentials provider for the specified role.
func (gw *Gateway) AssumeRole(role arn.ARN) *creds.Provider {
	return gw.proxy.AssumeRole(role, 0)
}

// ProxyExternalID returns the external id for the MasterRole in the current
// organization.
func ProxyExternalID(org *account.Org) string {
	if org.ID == "" {
		panic("awsx: unknown organization id")
	}
	var buf [64]byte
	b := append(buf[:0], "oktapus:"...)
	b = append(b, org.MasterID...)
	b = append(b, ':')
	b = append(b, org.MasterEmail...)
	h := hmac.New(sha512.New512_256, []byte(org.ID))
	h.Write(b)
	b = h.Sum(b[:0])
	return hex.EncodeToString(b)
}
