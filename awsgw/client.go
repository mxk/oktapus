package awsgw

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
	"github.com/aws/aws-sdk-go/aws/arn"
	"github.com/aws/aws-sdk-go/aws/client"
	orgs "github.com/aws/aws-sdk-go/service/organizations"
	"github.com/aws/aws-sdk-go/service/sts"
)

// Client provides access to multiple AWS accounts via one gateway account. It
// is not safe to use the client concurrently from multiple goroutines.
type Client struct {
	// GatewayCreds are the credentials used to access the gateway account.
	GatewayCreds CredsProvider

	// MasterRole is a master account role allowed to list org accounts.
	MasterRole string

	// CommonRole is the role to assume when accessing other accounts.
	CommonRole string

	sess    client.ConfigProvider
	org     *orgs.Organizations
	sts     *sts.STS
	ident   Ident
	orgInfo Org

	roleSessionName string

	cache map[string]*accountCtx
	saved *clientState
}

// NewClient creates a new AWS gateway client. The client is not usable until
// Connect() is called, which should be done after restoring any saved state.
func NewClient(sess client.ConfigProvider) *Client {
	return &Client{sess: sess}
}

// ConfigProvider returns the ConfigProvider that was passed to NewClient.
func (c *Client) ConfigProvider() client.ConfigProvider {
	return c.sess
}

// Connect establishes a connection to AWS, and gets client identity and
// organization information.
func (c *Client) Connect() error {
	if c.sts != nil {
		return errors.New("awsgw: already connected")
	}
	var cfg aws.Config
	if c.GatewayCreds != nil {
		cfg.Credentials = c.GatewayCreds.Creds()
	}

	// Get organization info
	orgClient := orgs.New(c.sess, &cfg)
	var org *orgs.DescribeOrganizationOutput
	var orgErr error
	var mu sync.Mutex
	mu.Lock()
	go func() {
		defer mu.Unlock()
		org, orgErr = orgClient.DescribeOrganization(nil)
	}()

	// Get caller info
	stsClient := sts.New(c.sess, &cfg)
	id, err := stsClient.GetCallerIdentity(nil)
	if err != nil {
		return err
	} else if mu.Lock(); orgErr != nil {
		return orgErr
	}

	c.sts, c.org = stsClient, orgClient
	c.ident.set(id)
	c.orgInfo.set(org)
	c.roleSessionName = getSessName(&c.ident)
	c.CommonRole = c.roleSessionName

	// If gateway account isn't master, change org client credentials
	if !c.IsMaster() {
		c.org.Config.Credentials = c.proxyCreds().Creds()
	}

	// Restore saved state
	if c.saved != nil && len(c.saved.Accounts) > 0 {
		acs := c.saved.Accounts
		if c.cache == nil {
			c.cache = make(map[string]*accountCtx, len(acs))
		}
		for i := range acs {
			id := acs[i].Account.ID
			c.cache[id] = acs[i].restore(c.credsProvider(id))
		}
	}
	c.saved = nil
	return nil
}

// Ident returns the identity of the gateway credentials.
func (c *Client) Ident() Ident {
	return c.ident
}

// OrgInfo returns information about the organization.
func (c *Client) OrgInfo() Org {
	return c.orgInfo
}

// IsMaster returns true if the gateway account is organization master.
func (c *Client) IsMaster() bool {
	return c.ident.AccountID == c.orgInfo.MasterAccountID &&
		c.ident.AccountID != ""
}

// OrgsClient returns the organizations API client. It returns nil if the
// gateway account is not the organization master.
func (c *Client) OrgsClient() *orgs.Organizations {
	if c.IsMaster() {
		return c.org
	}
	return nil
}

// Refresh updates information about all accounts in the organization.
func (c *Client) Refresh() error {
	valid := make(map[string]struct{}, len(c.cache))
	pager := func(out *orgs.ListAccountsOutput, lastPage bool) bool {
		for _, src := range out.Accounts {
			valid[c.Update(src).ID] = struct{}{}
		}
		return true
	}
	if err := c.org.ListAccountsPages(nil, pager); err != nil {
		return err
	}
	for id := range c.cache {
		if _, ok := valid[id]; !ok {
			delete(c.cache, id)
		}
	}
	return nil
}

// Accounts returns cached information about all accounts in the organization.
// The accounts are returned in random order.
func (c *Client) Accounts() []*Account {
	all := make([]*Account, 0, len(c.cache))
	for _, ac := range c.cache {
		all = append(all, &ac.Account)
	}
	return all
}

// Update updates account information from a more recent description.
func (c *Client) Update(src *orgs.Account) *Account {
	ac := c.getAccount(aws.StringValue(src.Id))
	ac.set(src)
	return &ac.Account
}

// CredsProvider returns a credentials provider for the specified account.
func (c *Client) CredsProvider(accountID string) CredsProvider {
	return c.getAccount(accountID).cp
}

// clientState contains saved Client state.
type clientState struct {
	MasterCreds *StaticCreds
	Accounts    []accountState
}

// GobEncode implements gob.GobEncoder interface.
func (c *Client) GobEncode() ([]byte, error) {
	if c.sts == nil {
		// If the client never connected, the old state (if any) hasn't changed
		return nil, nil
	}
	var s clientState
	if c.GatewayCreds != nil {
		s.MasterCreds = c.GatewayCreds.Save()
	}
	if len(c.cache) > 0 {
		acs := make([]accountState, 0, len(c.cache))
		for _, ac := range c.cache {
			acs = append(acs, accountState{&ac.Account, ac.cp.Save()})
		}
		s.Accounts = acs
	}
	var buf bytes.Buffer
	err := gob.NewEncoder(&buf).Encode(&s)
	return buf.Bytes(), err
}

// GobDecode implements gob.GobDecoder interface.
func (c *Client) GobDecode(b []byte) error {
	if len(b) == 0 {
		return nil
	}
	var s clientState
	if err := gob.NewDecoder(bytes.NewReader(b)).Decode(&s); err != nil {
		return err
	}
	if s.MasterCreds != nil && s.MasterCreds.valid() {
		c.GatewayCreds = s.MasterCreds
	}
	// TODO: Should expired master creds invalidate account creds?
	// Accounts cannot be restored until the client is connected
	c.saved = &s
	return nil
}

// credsProvider returns a credentials provider for the specified account id.
func (c *Client) credsProvider(id string) CredsProvider {
	role := roleARN(id, c.CommonRole)
	return NewAssumeRoleCreds(c.sts.AssumeRole, role, c.roleSessionName)
}

// getAccount returns an existing or new account context for the specified id.
// The caller must hold a lock on c.mu.
func (c *Client) getAccount(id string) *accountCtx {
	ac := c.cache[id]
	if ac == nil {
		ac = &accountCtx{Account{ID: id}, c.credsProvider(id)}
		if c.cache == nil {
			c.cache = make(map[string]*accountCtx)
		}
		c.cache[id] = ac
	}
	return ac
}

// proxyCreds returns credentials for the MasterRole.
func (c *Client) proxyCreds() *AssumeRoleCreds {
	if c.orgInfo.ID == "" {
		panic("awsgw: unknown organization id")
	}

	// MasterRole requires an external id, which is derived from org info
	var buf [64]byte
	b := append(buf[:0], "oktapus:"...)
	b = append(b, c.orgInfo.MasterAccountID...)
	b = append(b, ':')
	b = append(b, c.orgInfo.MasterAccountEmail...)
	h := hmac.New(sha512.New512_256, []byte(c.orgInfo.ID))
	h.Write(b)
	b = h.Sum(b[:0])

	role := roleARN(c.orgInfo.MasterAccountID, c.MasterRole)
	cr := NewAssumeRoleCreds(c.sts.AssumeRole, role, c.roleSessionName)
	cr.ExternalId = aws.String(hex.EncodeToString(b))
	return cr
}

// Ident contains data from sts:GetCallerIdentity API call.
type Ident struct{ AccountID, UserARN, UserID string }

// set updates identity information.
func (id *Ident) set(out *sts.GetCallerIdentityOutput) {
	*id = Ident{
		AccountID: aws.StringValue(out.Account),
		UserARN:   aws.StringValue(out.Arn),
		UserID:    aws.StringValue(out.UserId),
	}
}

// Org contains data from organizations:DescribeOrganization API call.
type Org struct {
	ARN                string
	FeatureSet         string
	ID                 string
	MasterAccountARN   string
	MasterAccountEmail string
	MasterAccountID    string
}

// set updates organization information.
func (o *Org) set(out *orgs.DescribeOrganizationOutput) {
	*o = Org{
		ARN:                aws.StringValue(out.Organization.Arn),
		FeatureSet:         aws.StringValue(out.Organization.FeatureSet),
		ID:                 aws.StringValue(out.Organization.Id),
		MasterAccountARN:   aws.StringValue(out.Organization.MasterAccountArn),
		MasterAccountEmail: aws.StringValue(out.Organization.MasterAccountEmail),
		MasterAccountID:    aws.StringValue(out.Organization.MasterAccountId),
	}
}

// getSessName derives RoleSessionName for new sessions from the current
// identity.
func getSessName(id *Ident) string {
	if i := strings.IndexByte(id.UserID, ':'); i != -1 {
		return id.UserID[i+1:]
	}
	if r, _ := arn.Parse(id.UserARN); strings.HasPrefix(r.Resource, "user/") {
		i := strings.LastIndexByte(r.Resource, '/')
		return r.Resource[i+1:]
	} else if r.Resource == "root" {
		return "OrganizationAccountAccessRole"
	}
	return id.UserID
}

// roleARN returns the IAM role ARN for the given account id and role name.
func roleARN(account, role string) string {
	return "arn:aws:iam::" + account + ":role/" + role
}
