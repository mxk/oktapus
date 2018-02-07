package mock

import (
	"sort"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/request"
	orgs "github.com/aws/aws-sdk-go/service/organizations"
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
			JoinedMethod:    aws.String(orgs.AccountJoinedMethodCreated),
			JoinedTimestamp: aws.Time(time.Unix(0, 0)),
			Name:            aws.String(name),
			Status:          aws.String(orgs.AccountStatusActive),
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
	acs[0].JoinedMethod = aws.String(orgs.AccountJoinedMethodInvited)
	acs[2].Status = aws.String(orgs.AccountStatusSuspended)
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
func (r OrgsRouter) AllAccounts() []*orgs.Account {
	acs := make([]*orgs.Account, 0, len(r))
	for _, ac := range r {
		cpy := ac.Account
		acs = append(acs, &cpy)
	}
	sort.Slice(acs, func(i, j int) bool {
		return aws.StringValue(acs[i].Id) < aws.StringValue(acs[j].Id)
	})
	return acs
}

// Route implements the Router interface.
func (r OrgsRouter) Route(s *Session, q *request.Request, api string) bool {
	switch api {
	case "organizations:DescribeOrganization":
		r.describeOrganization(q)
	case "organizations:ListAccounts":
		r.listAccounts(q)
	default:
		return r[reqAccountID(q)].Route(s, q, api)
	}
	return true
}

func (r OrgsRouter) describeOrganization(q *request.Request) {
	q.Data.(*orgs.DescribeOrganizationOutput).Organization = &orgs.Organization{
		Arn:                aws.String("arn:aws:organizations::000000000000:organization/o-test"),
		FeatureSet:         aws.String(orgs.OrganizationFeatureSetAll),
		Id:                 aws.String("o-test"),
		MasterAccountArn:   aws.String("arn:aws:organizations::000000000000:account/o-test/000000000000"),
		MasterAccountEmail: aws.String("master@example.com"),
		MasterAccountId:    aws.String("000000000000"),
	}
}

func (r OrgsRouter) listAccounts(q *request.Request) {
	requireMaster(q)
	q.Data.(*orgs.ListAccountsOutput).Accounts = r.AllAccounts()
}

func requireMaster(q *request.Request) {
	if reqAccountID(q) != "000000000000" {
		api := q.ClientInfo.ServiceName + ":" + q.Operation.Name
		panic("mock: " + api + " must be called from the master account")
	}
}
