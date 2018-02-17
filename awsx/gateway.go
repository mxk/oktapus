package awsx

import (
	"crypto/hmac"
	"crypto/sha512"
	"encoding/hex"
	"errors"
	"strings"
	"sync"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/client"
	orgs "github.com/aws/aws-sdk-go/service/organizations"
	orgsif "github.com/aws/aws-sdk-go/service/organizations/organizationsiface"
	"github.com/aws/aws-sdk-go/service/sts"
)

// Gateway provides access to multiple AWS accounts via one gateway account. It
// is not safe to use the client concurrently from multiple goroutines.
type Gateway struct {
	Creds CredsProvider // Gateway account credentials

	MasterRole ARN // Master account role with ListAccounts permission
	CommonRole ARN // Role to assume when accessing other accounts

	sess    client.ConfigProvider
	org     *orgs.Organizations
	sts     *sts.STS
	ident   Ident
	orgInfo Org

	roleSessionName string

	accounts map[string]*Account
	creds    map[string]CredsProvider
}

// NewGateway creates a new AWS gateway client. The client is not usable until
// Connect() is called, which should be done after restoring any saved state.
func NewGateway(sess client.ConfigProvider) *Gateway {
	return &Gateway{sess: sess}
}

// ClientConfig implements client.ConfigProvider via the provider passed to
// NewGateway.
func (gw *Gateway) ClientConfig(service string, cfgs ...*aws.Config) client.Config {
	return gw.sess.ClientConfig(service, cfgs...)
}

// Connect establishes a connection to AWS, and gets client identity and
// organization information.
func (gw *Gateway) Connect() error {
	if gw.sts != nil {
		return errors.New("awsx: already connected")
	}
	var cfg aws.Config
	if gw.Creds != nil {
		cfg.Credentials = gw.Creds.Creds()
	}

	// Get organization info
	orgClient := orgs.New(gw.sess, &cfg)
	var org *orgs.DescribeOrganizationOutput
	var orgErr error
	var mu sync.Mutex
	mu.Lock()
	go func() {
		defer mu.Unlock()
		org, orgErr = orgClient.DescribeOrganization(nil)
	}()

	// Get caller info
	stsClient := sts.New(gw.sess, &cfg)
	id, err := stsClient.GetCallerIdentity(nil)
	if err != nil {
		return err
	} else if mu.Lock(); orgErr != nil {
		return orgErr
	}

	gw.sts, gw.org = stsClient, orgClient
	gw.ident.Set(id)
	gw.orgInfo.Set(org)
	gw.roleSessionName = getSessName(&gw.ident)
	gw.CommonRole = NewARN(gw.ident.UserARN.Partition(), "iam", "", "",
		"role/", gw.roleSessionName)

	// If gateway account isn't master, change org client credentials
	if !gw.IsMaster() {
		gw.org.Config.Credentials = gw.proxyCreds().Creds()
	}
	return nil
}

// Ident returns the identity of the gateway credentials.
func (gw *Gateway) Ident() Ident {
	return gw.ident
}

// Org returns information about the organization.
func (gw *Gateway) Org() Org {
	return gw.orgInfo
}

// IsMaster returns true if the gateway account is organization master.
func (gw *Gateway) IsMaster() bool {
	return gw.ident.AccountID == gw.orgInfo.MasterID &&
		gw.ident.AccountID != ""
}

// OrgsClient returns the organizations API client. It returns nil if the
// gateway account is not the organization master.
func (gw *Gateway) OrgsClient() orgsif.OrganizationsAPI {
	if gw.IsMaster() {
		return gw.org
	}
	return nil
}

// Refresh updates information about all accounts in the organization.
func (gw *Gateway) Refresh() error {
	valid := make(map[string]struct{}, len(gw.accounts))
	pager := func(out *orgs.ListAccountsOutput, lastPage bool) bool {
		for _, src := range out.Accounts {
			ac := gw.Account(aws.StringValue(src.Id))
			ac.Set(src)
			valid[ac.ID] = struct{}{}
		}
		return true
	}
	if err := gw.org.ListAccountsPages(nil, pager); err != nil {
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
func (gw *Gateway) CredsProvider(accountID string) CredsProvider {
	cp := gw.creds[accountID]
	if cp == nil {
		if !IsAccountID(accountID) {
			panic("awsx: invalid account id: " + accountID)
		}
		cp = gw.AssumeRole(gw.CommonRole.WithAccount(accountID))
		if gw.creds == nil {
			gw.creds = make(map[string]CredsProvider)
		}
		gw.creds[accountID] = cp
	}
	return cp
}

// AssumeRole returns new AssumeRole credentials for the specified account ID
// and role name.
func (gw *Gateway) AssumeRole(role ARN) *AssumeRoleCreds {
	return NewAssumeRoleCreds(gw.sts.AssumeRole, role, gw.roleSessionName)
}

// proxyCreds returns credentials for the MasterRole.
func (gw *Gateway) proxyCreds() *AssumeRoleCreds {
	if gw.MasterRole == "" {
		panic("awsx: master role not set")
	}
	role := NewARN(gw.ident.UserARN.Partition(), "iam", "", gw.orgInfo.MasterID,
		"role", gw.MasterRole.Path(), gw.MasterRole.Name())
	cr := NewAssumeRoleCreds(gw.sts.AssumeRole, role, gw.roleSessionName)
	cr.ExternalId = aws.String(ProxyExternalID(&gw.orgInfo))
	return cr
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

// getSessName derives RoleSessionName for new sessions from the current
// identity.
func getSessName(id *Ident) string {
	if i := strings.IndexByte(id.UserID, ':'); i != -1 {
		return id.UserID[i+1:]
	}
	if id.UserARN.Type() == "user" {
		return id.UserARN.Name()
	} else if id.UserARN.Resource() == "root" {
		return "OrganizationAccountAccessRole"
	}
	return id.UserID
}
