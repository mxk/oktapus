package awsx

import (
	"crypto/hmac"
	"crypto/sha512"
	"encoding/hex"
	"errors"

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

	proxy     *creds.Proxy
	orgClient *orgs.Organizations
	orgInfo   Org

	accounts map[string]*Account
	creds    map[string]*creds.Provider
}

// NewGateway creates a new AWS gateway client. The client is not usable until
// Connect() is called, which should be done after restoring any saved state.
func NewGateway(cfg *aws.Config) *Gateway {
	return &Gateway{orgClient: orgs.New(*cfg)}
}

// Connect establishes a connection to AWS, and gets client identity and
// organization information.
func (gw *Gateway) Connect() error {
	if gw.proxy != nil {
		return errors.New("awsx: already connected")
	}
	var org *orgs.DescribeOrganizationOutput
	err := fast.Call(
		func() (err error) {
			org, err = gw.orgClient.DescribeOrganizationRequest(nil).Send()
			return
		},
		func() (err error) {
			gw.proxy, err = creds.NewProxy(&gw.orgClient.Config)
			return
		},
	)
	if err != nil {
		return err
	}

	gw.orgInfo.Set(org)
	gw.CommonRole = arn.New(gw.proxy.Ident.Partition(), "iam", "", "",
		"role/", gw.proxy.SessName)

	// If gateway account isn't master, change org client credentials
	if !gw.IsMaster() {
		if gw.MasterRole == "" {
			return errors.New("awsx: gateway master role not set")
		}
		gw.MasterRole = arn.New(gw.proxy.Ident.Partition(), "iam", "",
			gw.orgInfo.MasterID, "role", gw.MasterRole.Path(),
			gw.MasterRole.Name())
		gw.orgClient.Credentials = gw.proxy.Provider(&sts.AssumeRoleInput{
			RoleArn:         arn.String(gw.MasterRole),
			RoleSessionName: aws.String(gw.proxy.SessName),
			ExternalId:      aws.String(ProxyExternalID(&gw.orgInfo)),
		})
	}
	return nil
}

// Ident returns the identity of the gateway credentials.
func (gw *Gateway) Ident() creds.Ident {
	if gw.proxy != nil {
		return gw.proxy.Ident
	}
	return creds.Ident{}
}

// Org returns information about the organization.
func (gw *Gateway) Org() Org { return gw.orgInfo }

// IsMaster returns true if the gateway account is organization master.
func (gw *Gateway) IsMaster() bool {
	return gw.proxy.Ident.Account == gw.orgInfo.MasterID &&
		gw.orgInfo.MasterID != ""
}

// OrgsClient returns the organizations API client. It returns nil if the
// gateway account is not the organization master.
func (gw *Gateway) OrgsClient() *orgs.Organizations {
	if gw.IsMaster() {
		return gw.orgClient
	}
	return nil
}

// Refresh updates information about all accounts in the organization.
func (gw *Gateway) Refresh() error {
	valid := make(map[string]struct{}, len(gw.accounts))
	r := gw.orgClient.ListAccountsRequest(nil)
	p := r.Paginate()
	for p.Next() {
		out := p.CurrentPage()
		for i := range out.Accounts {
			src := &out.Accounts[i]
			ac := gw.Account(aws.StringValue(src.Id))
			ac.Set(src)
			valid[ac.ID] = struct{}{}
		}
	}
	if err := p.Err(); err != nil {
		return err
	}
	for id := range gw.accounts {
		if _, ok := valid[id]; !ok {
			delete(gw.accounts, id)
		}
	}
	return nil
}

// Accounts returns cached information about all accounts in the organization.
// The accounts are returned in random order.
func (gw *Gateway) Accounts() []*Account {
	all := make([]*Account, 0, len(gw.accounts))
	for _, ac := range gw.accounts {
		all = append(all, ac)
	}
	return all
}

// Account returns information for the specified account ID.
func (gw *Gateway) Account(id string) *Account {
	ac := gw.accounts[id]
	if ac == nil {
		if !IsAccountID(id) {
			panic("awsx: invalid account id: " + id)
		}
		if ac = (&Account{ID: id}); gw.accounts == nil {
			gw.accounts = make(map[string]*Account)
		}
		gw.accounts[id] = ac
	}
	return ac
}

// CredsProvider returns a credentials provider for the specified account ID.
func (gw *Gateway) CredsProvider(accountID string) *creds.Provider {
	cp := gw.creds[accountID]
	if cp == nil {
		if !IsAccountID(accountID) {
			panic("awsx: invalid account id: " + accountID)
		}
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
func ProxyExternalID(org *Org) string {
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
