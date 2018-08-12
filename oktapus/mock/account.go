package mock

import (
	"fmt"
	"sort"
	"strconv"
	"time"

	"github.com/LuminalHQ/cloudcover/x/arn"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	orgs "github.com/aws/aws-sdk-go-v2/service/organizations"
)

// Account is a mock AWS Organizations account.
type Account struct {
	orgs.Account
	ChainRouter
}

// NewAccount returns a new AWS Organizations account.
func NewAccount(ctx arn.Ctx, id, name string) *Account {
	id = AccountID(id)
	return &Account{
		Account: orgs.Account{
			Arn:             arn.String(ctx.New("organizations", "account/o-test/", id)),
			Email:           aws.String(name + "@example.com"),
			Id:              aws.String(id),
			JoinedMethod:    orgs.AccountJoinedMethodCreated,
			JoinedTimestamp: aws.Time(time.Unix(0, 0)),
			Name:            aws.String(name),
			Status:          orgs.AccountStatusActive,
		},
		ChainRouter: ChainRouter{UserRouter{}, RoleRouter{}},
	}
}

// AccountRouter handles Organizations API calls.
type AccountRouter map[string]*Account

// Route implements the Router interface.
func (r AccountRouter) Route(q *Request) bool {
	return RouteMethod(r, q) || r[q.Ctx.Account].Route(q)
}

// Add adds new accounts to the router.
func (r AccountRouter) Add(acs ...*Account) {
	for _, ac := range acs {
		r[*ac.Id] = ac
	}
}

// Get returns the account with the given id.
func (r AccountRouter) Get(id string) *Account {
	if ac := r[AccountID(id)]; ac != nil {
		return ac
	}
	panic("mock: invalid account id: " + id)
}

// AllAccounts returns all accounts sorted by account id.
func (r AccountRouter) AllAccounts() []orgs.Account {
	acs := make([]orgs.Account, 0, len(r))
	for _, ac := range r {
		acs = append(acs, ac.Account)
	}
	sort.Slice(acs, func(i, j int) bool {
		return aws.StringValue(acs[i].Id) < aws.StringValue(acs[j].Id)
	})
	return acs
}

func (r AccountRouter) CreateAccount(q *Request, in *orgs.CreateAccountInput) {
	requireMaster(q)
	var max uint64
	for id := range r {
		n, err := strconv.ParseUint(id, 10, 64)
		if err != nil {
			panic(err)
		}
		if n > max {
			max = n
		}
	}
	id := fmt.Sprintf("%.12d", max+1)
	ac := NewAccount(q.Ctx, id, aws.StringValue(in.AccountName))
	r[id] = ac
	ac.Email = in.Email
	role := aws.StringValue(in.RoleName)
	if role == "" {
		role = "OrganizationAccountAccessRole"
	}
	ac.RoleRouter()[role] = &Role{Role: iam.Role{
		Arn:      arn.String(q.Ctx.New("iam", "role/", role)),
		Path:     aws.String("/"),
		RoleName: aws.String(role),
	}}
	q.Data.(*orgs.CreateAccountOutput).CreateAccountStatus = &orgs.CreateAccountStatus{
		Id:    aws.String(id),
		State: orgs.CreateAccountStateInProgress,
	}
}

func (r AccountRouter) DescribeAccount(q *Request, in *orgs.DescribeAccountInput) {
	requireMaster(q)
	id := aws.StringValue(in.AccountId)
	ac := r.Get(id)
	cpy := ac.Account
	q.Data.(*orgs.DescribeAccountOutput).Account = &cpy
}

func (r AccountRouter) describeCreateAccountStatus(q *Request, in *orgs.DescribeCreateAccountStatusInput) {
	requireMaster(q)
	id := aws.StringValue(in.CreateAccountRequestId)
	ac := r.Get(id)
	q.Data.(*orgs.DescribeCreateAccountStatusOutput).CreateAccountStatus = &orgs.CreateAccountStatus{
		AccountId:   ac.Id,
		AccountName: ac.Name,
		State:       orgs.CreateAccountStateSucceeded,
	}
}

func (r AccountRouter) DescribeOrganization(q *Request, _ *orgs.DescribeOrganizationInput) {
	requireSupport(q)
	q.Data.(*orgs.DescribeOrganizationOutput).Organization = &orgs.Organization{
		Arn:                aws.String("arn:aws:organizations::000000000000:organization/o-test"),
		FeatureSet:         orgs.OrganizationFeatureSetAll,
		Id:                 aws.String("o-test"),
		MasterAccountArn:   aws.String("arn:aws:organizations::000000000000:account/o-test/000000000000"),
		MasterAccountEmail: aws.String("master@example.com"),
		MasterAccountId:    aws.String("000000000000"),
	}
}

func (r AccountRouter) ListAccounts(q *Request, _ *orgs.ListAccountsInput) {
	requireMaster(q)
	q.Data.(*orgs.ListAccountsOutput).Accounts = r.AllAccounts()
}

func requireSupport(q *Request) {
	if q.Ctx.Partition != "aws" {
		panic("mock: organizations api is not supported in " + q.Ctx.Partition)
	}
}

func requireMaster(q *Request) {
	requireSupport(q)
	if q.Ctx.Account != "000000000000" {
		api := q.Metadata.ServiceName + ":" + q.Operation.Name
		panic("mock: " + api + " must be called from the master account")
	}
}
