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

// OrgRouter handles Organizations API calls.
type OrgRouter map[string]*Account

// NewOrgRouter returns a router for a mock organization.
func NewOrgRouter() OrgRouter {
	r := OrgRouter{
		"000000000000": {Account: orgs.Account{
			Arn:             aws.String("arn:aws:organizations::000000000000:account/o-test/000000000000"),
			Email:           aws.String("master@example.com"),
			Id:              aws.String("000000000000"),
			JoinedMethod:    aws.String(orgs.AccountJoinedMethodInvited),
			JoinedTimestamp: aws.Time(time.Unix(0, 0)),
			Name:            aws.String("master"),
			Status:          aws.String(orgs.AccountStatusActive),
		}},
		"000000000001": {Account: orgs.Account{
			Arn:             aws.String("arn:aws:organizations::000000000000:account/o-test/000000000001"),
			Email:           aws.String("test1@example.com"),
			Id:              aws.String("000000000001"),
			JoinedMethod:    aws.String(orgs.AccountJoinedMethodCreated),
			JoinedTimestamp: aws.Time(time.Unix(1, 0)),
			Name:            aws.String("test1"),
			Status:          aws.String(orgs.AccountStatusActive),
		}},
		"000000000002": {Account: orgs.Account{
			Arn:             aws.String("arn:aws:organizations::000000000000:account/o-test/000000000002"),
			Email:           aws.String("test2@example.com"),
			Id:              aws.String("000000000002"),
			JoinedMethod:    aws.String(orgs.AccountJoinedMethodCreated),
			JoinedTimestamp: aws.Time(time.Unix(2, 0)),
			Name:            aws.String("test2"),
			Status:          aws.String(orgs.AccountStatusSuspended),
		}},
		"000000000003": {Account: orgs.Account{
			Arn:             aws.String("arn:aws:organizations::000000000000:account/o-test/000000000003"),
			Email:           aws.String("test3@example.com"),
			Id:              aws.String("000000000003"),
			JoinedMethod:    aws.String(orgs.AccountJoinedMethodCreated),
			JoinedTimestamp: aws.Time(time.Unix(3, 0)),
			Name:            aws.String("test3"),
			Status:          aws.String(orgs.AccountStatusActive),
		}},
	}
	for _, ac := range r {
		ac.ChainRouter.Add(RoleRouter{})
		ac.ChainRouter.Add(UserRouter{})
	}
	return r
}

// GetRouter returns the ChainRouter for the given account id.
func (r OrgRouter) GetRouter(id string) *ChainRouter {
	if ac := r[AccountID(id)]; ac != nil {
		return &ac.ChainRouter
	}
	panic("mock: invalid account id: " + id)
}

// GetAccounts returns all accounts sorted by account id.
func (r OrgRouter) GetAccounts() []*orgs.Account {
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
func (r OrgRouter) Route(s *Session, q *request.Request, api string) bool {
	switch api {
	case "organizations:DescribeOrganization":
		r.describeOrganization(q)
	case "organizations:ListAccounts":
		r.listAccounts(q)
	default:
		return r[getReqAccountID(q)].Route(s, q, api)
	}
	return true
}

func (r OrgRouter) describeOrganization(q *request.Request) {
	q.Data.(*orgs.DescribeOrganizationOutput).Organization = &orgs.Organization{
		Arn:                aws.String("arn:aws:organizations::000000000000:organization/o-test"),
		FeatureSet:         aws.String(orgs.OrganizationFeatureSetAll),
		Id:                 aws.String("o-test"),
		MasterAccountArn:   aws.String("arn:aws:organizations::000000000000:account/o-test/000000000000"),
		MasterAccountEmail: aws.String("master@example.com"),
		MasterAccountId:    aws.String("000000000000"),
	}
}

func (r OrgRouter) listAccounts(q *request.Request) {
	q.Data.(*orgs.ListAccountsOutput).Accounts = r.GetAccounts()
}
