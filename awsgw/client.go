package awsgw

import (
	"bytes"
	"encoding/gob"
	"errors"
	"strings"
	"sync"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/arn"
	"github.com/aws/aws-sdk-go/aws/client"
	orgs "github.com/aws/aws-sdk-go/service/organizations"
	"github.com/aws/aws-sdk-go/service/sts"
)

// Client provides access to multiple AWS accounts via one gateway account
// (usually the organization's master account). It is not safe to use the client
// concurrently from multiple goroutines.
type Client struct {
	MasterCreds CredsProvider
	CommonRole  string
	NoNegCache  bool // TODO: Implement

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

// Connect establishes a connection to AWS and gets client identity information.
func (c *Client) Connect() error {
	if c.sts != nil {
		return errors.New("awsgw: already connected")
	}
	var cfg aws.Config
	if c.MasterCreds != nil {
		cfg.Credentials = c.MasterCreds.Creds()
	}
	var wg sync.WaitGroup
	wg.Add(2)

	// TODO: Set timeouts via aws.Context?
	stsClient := sts.New(c.sess, &cfg)
	var id *sts.GetCallerIdentityOutput
	var idErr error
	go func() {
		defer wg.Done()
		id, idErr = stsClient.GetCallerIdentity(nil)
	}()

	orgClient := orgs.New(c.sess, &cfg)
	var org *orgs.DescribeOrganizationOutput
	var orgErr error
	go func() {
		defer wg.Done()
		org, orgErr = orgClient.DescribeOrganization(nil)
	}()

	if wg.Wait(); idErr != nil {
		return idErr
	}
	c.sts, c.org = stsClient, orgClient
	c.ident.set(id)
	if orgErr == nil {
		c.orgInfo.set(org)
	}
	c.roleSessionName = getSessName(&c.ident)
	c.CommonRole = c.roleSessionName

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

// Ident returns the identity of the master credentials.
func (c *Client) Ident() Ident {
	return c.ident
}

// OrgInfo returns information about the organization.
func (c *Client) OrgInfo() Org {
	return c.orgInfo
}

// OrgClient returns the organizations API client.
func (c *Client) OrgClient() *orgs.Organizations {
	return c.org
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
	if c.MasterCreds != nil {
		s.MasterCreds = c.MasterCreds.Save()
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
		c.MasterCreds = s.MasterCreds
	}
	// TODO: Should expired master creds invalidate account creds?
	// Accounts cannot be restored until the client is connected
	c.saved = &s
	return nil
}

// credsProvider returns a credentials provider for the specified account id.
func (c *Client) credsProvider(id string) CredsProvider {
	role := "arn:aws:iam::" + id + ":role/" + c.CommonRole
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
