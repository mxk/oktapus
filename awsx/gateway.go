package awsx

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha512"
	"encoding/gob"
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

	cache map[string]*accountCtx
	saved *clientState
}

// NewGateway creates a new AWS gateway client. The client is not usable until
// Connect() is called, which should be done after restoring any saved state.
func NewGateway(sess client.ConfigProvider) *Gateway {
	return &Gateway{sess: sess}
}

// ConfigProvider returns the ConfigProvider that was passed to NewGateway.
func (gw *Gateway) ConfigProvider() client.ConfigProvider {
	return gw.sess
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
	gw.ident.set(id)
	gw.orgInfo.set(org)
	gw.roleSessionName = getSessName(&gw.ident)
	gw.CommonRole = NewARN(gw.ident.UserARN.Partition(), "iam", "", "",
		"role/", gw.roleSessionName)

	// If gateway account isn't master, change org client credentials
	if !gw.IsMaster() {
		gw.org.Config.Credentials = gw.proxyCreds().Creds()
	}

	// Restore saved state
	if gw.saved != nil && len(gw.saved.Accounts) > 0 {
		acs := gw.saved.Accounts
		if gw.cache == nil {
			gw.cache = make(map[string]*accountCtx, len(acs))
		}
		for i := range acs {
			id := acs[i].Account.ID
			gw.cache[id] = acs[i].restore(gw.credsProvider(id))
		}
	}
	gw.saved = nil
	return nil
}

// Ident returns the identity of the gateway credentials.
func (gw *Gateway) Ident() Ident {
	return gw.ident
}

// OrgInfo returns information about the organization.
func (gw *Gateway) OrgInfo() Org {
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
	valid := make(map[string]struct{}, len(gw.cache))
	pager := func(out *orgs.ListAccountsOutput, lastPage bool) bool {
		for _, src := range out.Accounts {
			valid[gw.Update(src).ID] = struct{}{}
		}
		return true
	}
	if err := gw.org.ListAccountsPages(nil, pager); err != nil {
		return err
	}
	for id := range gw.cache {
		if _, ok := valid[id]; !ok {
			delete(gw.cache, id)
		}
	}
	return nil
}

// Accounts returns cached information about all accounts in the organization.
// The accounts are returned in random order.
func (gw *Gateway) Accounts() []*Account {
	all := make([]*Account, 0, len(gw.cache))
	for _, ac := range gw.cache {
		all = append(all, &ac.Account)
	}
	return all
}

// Update updates account information from a more recent description.
func (gw *Gateway) Update(src *orgs.Account) *Account {
	ac := gw.getAccount(aws.StringValue(src.Id))
	ac.set(src)
	return &ac.Account
}

// CredsProvider returns a credentials provider for the specified account.
func (gw *Gateway) CredsProvider(accountID string) CredsProvider {
	return gw.getAccount(accountID).cp
}

// AssumeRole returns new AssumeRole credentials for the specified account ID
// and role name.
func (gw *Gateway) AssumeRole(role ARN) *AssumeRoleCreds {
	return NewAssumeRoleCreds(gw.sts.AssumeRole, role, gw.roleSessionName)
}

// clientState contains saved Gateway state.
type clientState struct {
	MasterCreds *StaticCreds
	Accounts    []accountState
}

// GobEncode implements gob.GobEncoder interface.
func (gw *Gateway) GobEncode() ([]byte, error) {
	if gw.sts == nil {
		// If the client never connected, the old state (if any) hasn't changed
		return nil, nil
	}
	var s clientState
	if gw.Creds != nil {
		s.MasterCreds = gw.Creds.Save()
	}
	if len(gw.cache) > 0 {
		acs := make([]accountState, 0, len(gw.cache))
		for _, ac := range gw.cache {
			acs = append(acs, accountState{&ac.Account, ac.cp.Save()})
		}
		s.Accounts = acs
	}
	var buf bytes.Buffer
	err := gob.NewEncoder(&buf).Encode(&s)
	return buf.Bytes(), err
}

// GobDecode implements gob.GobDecoder interface.
func (gw *Gateway) GobDecode(b []byte) error {
	if len(b) == 0 {
		return nil
	}
	var s clientState
	if err := gob.NewDecoder(bytes.NewReader(b)).Decode(&s); err != nil {
		return err
	}
	if s.MasterCreds != nil && s.MasterCreds.valid() {
		gw.Creds = s.MasterCreds
	}
	// TODO: Should expired master creds invalidate account creds?
	// Accounts cannot be restored until the client is connected
	gw.saved = &s
	return nil
}

// credsProvider returns a credentials provider for the specified account id.
func (gw *Gateway) credsProvider(id string) CredsProvider {
	role := gw.CommonRole.WithAccount(id)
	return NewAssumeRoleCreds(gw.sts.AssumeRole, role, gw.roleSessionName)
}

// getAccount returns an existing or new account context for the specified id.
// The caller must hold a lock on c.mu.
func (gw *Gateway) getAccount(id string) *accountCtx {
	ac := gw.cache[id]
	if ac == nil {
		ac = &accountCtx{Account{ID: id}, gw.credsProvider(id)}
		if gw.cache == nil {
			gw.cache = make(map[string]*accountCtx)
		}
		gw.cache[id] = ac
	}
	return ac
}

// proxyCreds returns credentials for the MasterRole.
func (gw *Gateway) proxyCreds() *AssumeRoleCreds {
	if gw.MasterRole == "" {
		panic("awsx: master role not set")
	}
	role := gw.MasterRole.WithAccount(gw.orgInfo.MasterID)
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

// Ident contains data from sts:GetCallerIdentity API call.
type Ident struct {
	AccountID string
	UserARN   ARN
	UserID    string
}

// set updates identity information.
func (id *Ident) set(out *sts.GetCallerIdentityOutput) {
	*id = Ident{
		AccountID: aws.StringValue(out.Account),
		UserARN:   ARNValue(out.Arn),
		UserID:    aws.StringValue(out.UserId),
	}
}

// Org contains data from organizations:DescribeOrganization API call.
type Org struct {
	ARN         ARN
	FeatureSet  string
	ID          string
	MasterARN   ARN
	MasterEmail string
	MasterID    string
}

// set updates organization information.
func (o *Org) set(out *orgs.DescribeOrganizationOutput) {
	*o = Org{
		ARN:         ARNValue(out.Organization.Arn),
		FeatureSet:  aws.StringValue(out.Organization.FeatureSet),
		ID:          aws.StringValue(out.Organization.Id),
		MasterARN:   ARNValue(out.Organization.MasterAccountArn),
		MasterEmail: aws.StringValue(out.Organization.MasterAccountEmail),
		MasterID:    aws.StringValue(out.Organization.MasterAccountId),
	}
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
