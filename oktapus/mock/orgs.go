package mock

import (
	"fmt"
	"sort"
	"strconv"
	"time"

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
func NewAccount(id, name string) *Account {
	id = AccountID(id)
	arn := "arn:aws:organizations::000000000000:account/o-test/" + id
	return &Account{
		Account: orgs.Account{
			Arn:             aws.String(arn),
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

// OrgsRouter handles Organizations API calls.
type OrgsRouter map[string]*Account

// NewOrgsRouter returns a router for a mock organization.
func NewOrgsRouter() OrgsRouter {
	acs := []*Account{
		NewAccount("000000000000", "master"),
		NewAccount("000000000001", "test1"),
		NewAccount("000000000002", "test2"),
		NewAccount("000000000003", "test3"),
	}
	acs[0].JoinedMethod = orgs.AccountJoinedMethodInvited
	acs[2].Status = orgs.AccountStatusSuspended
	acs[3].JoinedTimestamp = aws.Time(time.Unix(1, 0))
	r := make(OrgsRouter, len(acs))
	for _, ac := range acs {
		r[*ac.Id] = ac
	}
	return r
}

// Account returns the account with the given id.
func (r OrgsRouter) Account(id string) *Account {
	if ac := r[AccountID(id)]; ac != nil {
		return ac
	}
	panic("mock: invalid account id: " + id)
}

// AllAccounts returns all accounts sorted by account id.
func (r OrgsRouter) AllAccounts() []orgs.Account {
	acs := make([]orgs.Account, 0, len(r))
	for _, ac := range r {
		acs = append(acs, ac.Account)
	}
	sort.Slice(acs, func(i, j int) bool {
		return aws.StringValue(acs[i].Id) < aws.StringValue(acs[j].Id)
	})
	return acs
}

// Route implements the Router interface.
func (r OrgsRouter) Route(q *aws.Request) bool {
	switch q.Params.(type) {
	case *orgs.CreateAccountInput:
		r.createAccount(q)
	case *orgs.DescribeAccountInput:
		r.describeAccount(q)
	case *orgs.DescribeCreateAccountStatusInput:
		r.describeCreateAccountStatus(q)
	case *orgs.DescribeOrganizationInput:
		r.describeOrganization(q)
	case *orgs.ListAccountsInput:
		r.listAccounts(q)
	default:
		return r[reqAccountID(q)].Route(q)
	}
	return true
}

func (r OrgsRouter) createAccount(q *aws.Request) {
	requireMaster(q)
	in := q.Params.(*orgs.CreateAccountInput)
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
	ac := NewAccount(id, aws.StringValue(in.AccountName))
	r[id] = ac
	ac.Email = in.Email
	role := aws.StringValue(in.RoleName)
	if role == "" {
		role = "OrganizationAccountAccessRole"
	}
	ac.RoleRouter()[role] = &Role{Role: iam.Role{
		Arn:      aws.String(RoleARN("", role)),
		Path:     aws.String("/"),
		RoleName: aws.String(role),
	}}
	q.Data.(*orgs.CreateAccountOutput).CreateAccountStatus = &orgs.CreateAccountStatus{
		Id:    aws.String(id),
		State: orgs.CreateAccountStateInProgress,
	}
}

func (r OrgsRouter) describeAccount(q *aws.Request) {
	requireMaster(q)
	id := aws.StringValue(q.Params.(*orgs.DescribeAccountInput).AccountId)
	ac := r.Account(id)
	cpy := ac.Account
	q.Data.(*orgs.DescribeAccountOutput).Account = &cpy
}

func (r OrgsRouter) describeCreateAccountStatus(q *aws.Request) {
	requireMaster(q)
	id := aws.StringValue(q.Params.(*orgs.DescribeCreateAccountStatusInput).
		CreateAccountRequestId)
	ac := r.Account(id)
	q.Data.(*orgs.DescribeCreateAccountStatusOutput).CreateAccountStatus = &orgs.CreateAccountStatus{
		AccountId:   ac.Id,
		AccountName: ac.Name,
		State:       orgs.CreateAccountStateSucceeded,
	}
}

func (r OrgsRouter) describeOrganization(q *aws.Request) {
	q.Data.(*orgs.DescribeOrganizationOutput).Organization = &orgs.Organization{
		Arn:                aws.String("arn:aws:organizations::000000000000:organization/o-test"),
		FeatureSet:         orgs.OrganizationFeatureSetAll,
		Id:                 aws.String("o-test"),
		MasterAccountArn:   aws.String("arn:aws:organizations::000000000000:account/o-test/000000000000"),
		MasterAccountEmail: aws.String("master@example.com"),
		MasterAccountId:    aws.String("000000000000"),
	}
}

func (r OrgsRouter) listAccounts(q *aws.Request) {
	requireMaster(q)
	q.Data.(*orgs.ListAccountsOutput).Accounts = r.AllAccounts()
}

func requireMaster(q *aws.Request) {
	if reqAccountID(q) != "000000000000" {
		api := q.Metadata.ServiceName + ":" + q.Operation.Name
		panic("mock: " + api + " must be called from the master account")
	}
}
