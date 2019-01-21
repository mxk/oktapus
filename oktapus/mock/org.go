package mock

import (
	"sort"
	"strconv"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	orgs "github.com/aws/aws-sdk-go-v2/service/organizations"
	"github.com/mxk/cloudcover/x/arn"
)

// OrgRouter handles Organizations API calls.
type OrgRouter struct {
	orgs.Organization
	Accounts map[string]*orgs.Account
	Next     uint64
}

// NewOrg creates a new AWS organization router consisting of a master account
// and any number of additional non-master accounts. Account IDs are assigned
// sequentially starting from ctx.Account.
func NewOrg(ctx arn.Ctx, master string, names ...string) *OrgRouter {
	masterID, err := strconv.ParseUint(ctx.Account, 10, 64)
	if err != nil {
		panic(err)
	}
	r := &OrgRouter{
		Organization: orgs.Organization{
			Arn:                arn.String(ctx.New("organizations", "organization/o-", master)),
			FeatureSet:         orgs.OrganizationFeatureSetAll,
			Id:                 aws.String("o-" + master),
			MasterAccountArn:   arn.String(ctx.New("organizations", "account/o-", master, "/", ctx.Account)),
			MasterAccountEmail: aws.String(master + "@example.com"),
			MasterAccountId:    aws.String(ctx.Account),
		},
		Accounts: make(map[string]*orgs.Account, 1+len(names)),
		Next:     masterID,
	}
	r.NewAccount(master)
	for _, name := range names {
		r.NewAccount(name)
	}
	return r
}

// Route implements the Router interface.
func (r *OrgRouter) Route(q *Request) bool { return RouteMethod(r, q) }

// NewAccount adds a new account to the organization.
func (r *OrgRouter) NewAccount(name string) *orgs.Account {
	id := AccountID(strconv.FormatUint(r.Next, 10))
	r.Next++
	ac := &orgs.Account{
		Arn:             arn.String(arn.Value(r.MasterAccountArn).WithName(id)),
		Email:           aws.String(name + "@example.com"),
		Id:              aws.String(id),
		JoinedMethod:    orgs.AccountJoinedMethodCreated,
		JoinedTimestamp: aws.Time(time.Unix(0, 0)),
		Name:            aws.String(name),
		Status:          orgs.AccountStatusActive,
	}
	r.Accounts[id] = ac
	return ac
}

// Get returns the account with the given id.
func (r *OrgRouter) Get(id string) *orgs.Account {
	id = AccountID(id)
	if ac := r.Accounts[id]; ac != nil {
		return ac
	}
	panic("mock: unknown account id: " + id)
}

// All returns all accounts sorted by account id.
func (r *OrgRouter) All() []*orgs.Account {
	acs := make([]*orgs.Account, 0, len(r.Accounts))
	for _, ac := range r.Accounts {
		acs = append(acs, ac)
	}
	sort.Slice(acs, func(i, j int) bool {
		return aws.StringValue(acs[i].Id) < aws.StringValue(acs[j].Id)
	})
	return acs
}

func (r *OrgRouter) CreateAccount(q *Request, in *orgs.CreateAccountInput) {
	r.requireMaster(q)
	ac := r.NewAccount(aws.StringValue(in.AccountName))
	id := *ac.Id
	ac.Email = in.Email
	r.Accounts[id] = ac
	role := aws.StringValue(in.RoleName)
	if role == "" {
		role = "OrganizationAccountAccessRole"
	}
	q.AWS.Account(id).RoleRouter()[role] = &Role{Role: iam.Role{
		Arn:      arn.String(q.Ctx.New("iam", "role/", role)),
		Path:     aws.String("/"),
		RoleName: aws.String(role),
	}}
	q.Data.(*orgs.CreateAccountOutput).CreateAccountStatus = &orgs.CreateAccountStatus{
		Id:    aws.String(id),
		State: orgs.CreateAccountStateInProgress,
	}
}

func (r *OrgRouter) DescribeAccount(q *Request, in *orgs.DescribeAccountInput) {
	r.requireMaster(q)
	ac := *r.Get(aws.StringValue(in.AccountId))
	q.Data.(*orgs.DescribeAccountOutput).Account = &ac
}

func (r *OrgRouter) DescribeCreateAccountStatus(q *Request, in *orgs.DescribeCreateAccountStatusInput) {
	r.requireMaster(q)
	id := aws.StringValue(in.CreateAccountRequestId)
	ac := r.Get(id)
	q.Data.(*orgs.DescribeCreateAccountStatusOutput).CreateAccountStatus = &orgs.CreateAccountStatus{
		AccountId:   ac.Id,
		AccountName: ac.Name,
		State:       orgs.CreateAccountStateSucceeded,
	}
}

func (r *OrgRouter) DescribeOrganization(q *Request, _ *orgs.DescribeOrganizationInput) {
	org := r.Organization
	q.Data.(*orgs.DescribeOrganizationOutput).Organization = &org
}

func (r *OrgRouter) ListAccounts(q *Request, _ *orgs.ListAccountsInput) {
	r.requireMaster(q)
	all := r.All()
	acs := make([]orgs.Account, len(all))
	for i := range all {
		acs[i] = *all[i]
	}
	q.Data.(*orgs.ListAccountsOutput).Accounts = acs
}

func (r *OrgRouter) requireMaster(q *Request) {
	if q.Ctx.Account != aws.StringValue(r.MasterAccountId) {
		panic("mock: " + q.Name() + " must be called from the master account")
	}
}
