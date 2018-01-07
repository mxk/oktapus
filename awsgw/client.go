package awsgw

import (
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

const AssumeRolePolicy = `{
	"Version": "2012-10-17",
	"Statement": [{
		"Effect": "Allow",
		"Principal": {"AWS": "arn:aws:iam::%s:root"},
		"Action": "sts:AssumeRole",
		"Condition": {}
	}]
}`

const AdminPolicy = `{
	"Version": "2012-10-17",
	"Statement": [{
		"Effect": "Allow",
		"Action": "*",
		"Resource": "*"
	}]
}`

// Client provides access to multiple AWS accounts via one gateway account
// (usually the organization's master account).
type Client struct {
	CommonRole string
	AccountID  string
	UserARN    string
	UserID     string

	NoNegCache bool // TODO: Implement

	roleSessionName string

	sess  client.ConfigProvider
	creds *credentials.Credentials
	org   *orgs.Organizations
	sts   *sts.STS

	mu    sync.Mutex // TODO: Not acquired everywhere where it should be
	cache map[string]*accountCtx
}

// NewClient creates a new AWS gateway client.
func NewClient(cp client.ConfigProvider, cr *credentials.Credentials) (*Client, error) {
	var cfg aws.Config
	if cr != nil {
		cfg.Credentials = cr
	}
	c := &Client{
		sess:  cp,
		org:   orgs.New(cp, &cfg),
		sts:   sts.New(cp, &cfg),
		cache: make(map[string]*accountCtx),
	}
	err := c.whoAmI()
	if err != nil {
		c = nil
	}
	return c, err
}

// Refresh updates information about all accounts in the organization.
func (c *Client) Refresh() error {
	valid := make(map[string]struct{})
	pager := func(out *orgs.ListAccountsOutput, lastPage bool) bool {
		c.mu.Lock()
		defer c.mu.Unlock()
		for _, src := range out.Accounts {
			ac := c.getAccount(aws.StringValue(src.Id))
			ac.info.update(src)
			valid[ac.info.ID] = struct{}{}
		}
		return true
	}
	err := c.org.ListAccountsPages(nil, pager)
	// TODO: Don't clear if error? Check error code.
	c.mu.Lock()
	defer c.mu.Unlock()
	for id := range c.cache {
		if _, ok := valid[id]; !ok {
			delete(c.cache, id)
		}
	}
	return err
}

// Accounts returns cached information about all accounts in the organization.
// The accounts are returned in random order.
func (c *Client) Accounts() []*Account {
	c.mu.Lock()
	defer c.mu.Unlock()
	all := make([]*Account, 0, len(c.cache))
	for _, ac := range c.cache {
		all = append(all, &ac.info)
	}
	return all
}

// Creds returns credentials for the specified account.
func (c *Client) Creds(accountID string) *credentials.Credentials {
	return c.getAccountUnlocked(accountID).creds
}

// IAM creates a new IAM client for the specified account.
func (c *Client) IAM(accountID string) *iam.IAM {
	ac := c.getAccountUnlocked(accountID)
	cfg := aws.Config{Credentials: ac.creds}
	return iam.New(c.sess, &cfg)
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

// CreateAdminRole creates an admin account role that trusts the master account.
// If role name is not specified, it defaults to OrganizationAccountAccessRole.
func (c *Client) CreateAdminRole(accountID, name string) error {
	if name == "" {
		name = "OrganizationAccountAccessRole"
	}
	// TODO: Verify that accountID is master.
	policy := fmt.Sprintf(AssumeRolePolicy, c.AccountID)
	in := iam.CreateRoleInput{
		AssumeRolePolicyDocument: aws.String(policy),
		RoleName:                 aws.String(name),
	}
	im := c.IAM(accountID)
	if _, err := im.CreateRole(&in); err != nil {
		return err
	}
	// TODO: Use existing roles?
	in2 := iam.PutRolePolicyInput{
		PolicyDocument: aws.String(AdminPolicy),
		PolicyName:     aws.String("AdministratorAccess"),
		RoleName:       aws.String(name),
	}
	// TODO: Delete role if error?
	_, err := im.PutRolePolicy(&in2)
	return err
}

// whoAmI retrieves information about the current user or role.
func (c *Client) whoAmI() error {
	id, err := c.sts.GetCallerIdentity(nil)
	if err != nil {
		return err
	}
	c.AccountID = aws.StringValue(id.Account)
	c.UserARN = aws.StringValue(id.Arn)
	c.UserID = aws.StringValue(id.UserId)

	// RoleSessionName for new sessions is derived from the original credentials
	if i := strings.IndexByte(c.UserID, ':'); i != -1 {
		c.roleSessionName = c.UserID[i+1:]
	} else if r, _ := arn.Parse(c.UserARN); strings.HasPrefix(r.Resource, "user/") {
		c.roleSessionName = r.Resource[5:]
	} else {
		c.roleSessionName = c.UserID
	}

	// TODO: Get org info

	c.CommonRole = c.roleSessionName
	return nil
}

// getAccountUnlocked returns an existing or new account context for the
// specified id.
func (c *Client) getAccountUnlocked(id string) *accountCtx {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.getAccount(id)
}

// getAccount returns an existing or new account context for the specified id.
// The caller must hold a lock on c.mu.
func (c *Client) getAccount(id string) *accountCtx {
	ac := c.cache[id]
	if ac == nil {
		ac = newAccountCtx(c, id)
		c.cache[id] = ac
	}
	return ac
}
