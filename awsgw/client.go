package awsgw

import (
	"bytes"
	"encoding/gob"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/arn"
	"github.com/aws/aws-sdk-go/aws/client"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/service/iam"
	orgs "github.com/aws/aws-sdk-go/service/organizations"
	"github.com/aws/aws-sdk-go/service/sts"
)

const assumeRolePolicy = `{
	"Version": "2012-10-17",
	"Statement": [{
		"Effect": "Allow",
		"Principal": {"AWS": "arn:aws:iam::%s:root"},
		"Action": "sts:AssumeRole",
		"Condition": {}
	}]
}`

const adminPolicy = `{
	"Version": "2012-10-17",
	"Statement": [{
		"Effect": "Allow",
		"Action": "*",
		"Resource": "*"
	}]
}`

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
	ident   ident
	orgInfo org

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
		cfg.Credentials = credentials.NewCredentials(c.MasterCreds)
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

// Refresh updates information about all accounts in the organization.
func (c *Client) Refresh() error {
	valid := make(map[string]struct{}, len(c.cache))
	pager := func(out *orgs.ListAccountsOutput, lastPage bool) bool {
		for _, src := range out.Accounts {
			ac := c.getAccount(aws.StringValue(src.Id))
			ac.set(src)
			valid[ac.ID] = struct{}{}
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

// Creds returns credentials for the specified account.
func (c *Client) Creds(accountID string) *credentials.Credentials {
	return c.getAccount(accountID).creds
}

// CreateAccount creates a new account in the organization and returns the
// account ID.
func (c *Client) CreateAccount(name, email string) (string, error) {
	// TODO: Enforce 5 concurrent account creation requests
	in := orgs.CreateAccountInput{
		AccountName: aws.String(name),
		Email:       aws.String(email),
		RoleName:    aws.String(c.CommonRole),
	}
	out, err := c.org.CreateAccount(&in)
	if err != nil {
		return "", err
	}
	status := out.CreateAccountStatus
	state := aws.StringValue(status.State)
	in2 := orgs.DescribeCreateAccountStatusInput{
		CreateAccountRequestId: status.Id,
	}
	// TODO: Timeout, cancellation
	for state == orgs.CreateAccountStateInProgress {
		time.Sleep(2 * time.Second)
		out, err := c.org.DescribeCreateAccountStatus(&in2)
		if err != nil {
			return "", err
		}
		status = out.CreateAccountStatus
		state = aws.StringValue(status.State)
	}
	if state == orgs.CreateAccountStateSucceeded {
		return aws.StringValue(status.AccountId), nil
	}
	// TODO: Use awserr.Error?
	return "", fmt.Errorf("account creation failed (%s)",
		aws.StringValue(status.FailureReason))
}

// MasterAssumeRolePolicy returns AssumeRolePolicyDocument for iam:CreateRole
// API call.
func (c *Client) MasterAssumeRolePolicy() string {
	return fmt.Sprintf(assumeRolePolicy, c.orgInfo.MasterAccountID)
}

// CreateAdminRole creates an admin account role that trusts the master account.
// If role name is not specified, it defaults to OrganizationAccountAccessRole.
func (c *Client) CreateAdminRole(accountID, name string) error {
	if c.orgInfo.MasterAccountID == "" {
		return errors.New("awsgw: master account id unknown")
	}
	if name == "" {
		name = "OrganizationAccountAccessRole"
	}
	in := iam.CreateRoleInput{
		AssumeRolePolicyDocument: aws.String(c.MasterAssumeRolePolicy()),
		RoleName:                 aws.String(name),
	}
	im := c.iam(accountID)
	if _, err := im.CreateRole(&in); err != nil {
		return err
	}
	// TODO: Use existing policy?
	in2 := iam.PutRolePolicyInput{
		PolicyDocument: aws.String(adminPolicy),
		PolicyName:     aws.String("AdministratorAccess"),
		RoleName:       aws.String(name),
	}
	// TODO: Delete role if error?
	_, err := im.PutRolePolicy(&in2)
	return err
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
			acs = append(acs, accountState{&ac.Account, ac.Save()})
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
		ac = newAccountCtx(id, c.credsProvider(id))
		if c.cache == nil {
			c.cache = make(map[string]*accountCtx)
		}
		c.cache[id] = ac
	}
	return ac
}

// iam returns a new IAM client for the specified account id.
func (c *Client) iam(id string) *iam.IAM {
	cfg := aws.Config{Credentials: c.getAccount(id).creds}
	return iam.New(c.sess, &cfg)
}

// ident contains data from sts:GetCallerIdentity API call.
type ident struct{ AccountID, UserARN, UserID string }

// set updates identity information.
func (id *ident) set(out *sts.GetCallerIdentityOutput) {
	*id = ident{
		AccountID: aws.StringValue(out.Account),
		UserARN:   aws.StringValue(out.Arn),
		UserID:    aws.StringValue(out.UserId),
	}
}

// org contains data from organizations:DescribeOrganization API call.
type org struct {
	ARN                string
	FeatureSet         string
	ID                 string
	MasterAccountARN   string
	MasterAccountEmail string
	MasterAccountID    string
}

// set updates organization information.
func (o *org) set(out *orgs.DescribeOrganizationOutput) {
	*o = org{
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
func getSessName(id *ident) string {
	if i := strings.IndexByte(id.UserID, ':'); i != -1 {
		return id.UserID[i+1:]
	}
	if r, _ := arn.Parse(id.UserARN); strings.HasPrefix(r.Resource, "user/") {
		return r.Resource[5:]
	}
	return id.UserID
}
