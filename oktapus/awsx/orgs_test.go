package awsx

import (
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	orgs "github.com/aws/aws-sdk-go-v2/service/organizations"
	"github.com/mxk/cloudcover/oktapus/mock"
	"github.com/mxk/cloudcover/x/fast"
	"github.com/stretchr/testify/assert"
)

func TestCreateAccounts(t *testing.T) {
	w := mock.NewAWS(mock.Ctx, testOrg{})
	fast.MockSleep(-1)
	defer fast.MockSleep(0)

	in := []*orgs.CreateAccountInput{{
		AccountName: aws.String("a"),
		Email:       aws.String("test@example.com"),
	}, {
		AccountName: aws.String("b"),
		Email:       aws.String("test@example.com"),
	}, {
		AccountName: aws.String("c"),
		Email:       aws.String("test@example.com"),
	}}
	var a, b, c *orgs.Account
	for r := range CreateAccounts(*orgs.New(w.Cfg), in) {
		switch aws.StringValue(r.Name) {
		case "a":
			assert.NoError(t, r.Err)
			a = r.Account
		case "b":
			assert.NoError(t, r.Err)
			b = r.Account
		case "c":
			assert.EqualError(t, r.Err, "INTERNAL_FAILURE: account creation failed")
			c = new(orgs.Account)
		}
	}
	assert.NotNil(t, a)
	assert.NotNil(t, b)
	assert.NotNil(t, c)
}

type testOrg struct{}

func (r testOrg) Route(q *mock.Request) bool { return mock.RouteMethod(r, q) }

func (testOrg) CreateAccount(q *mock.Request, in *orgs.CreateAccountInput) {
	var s *orgs.CreateAccountStatus
	switch name := aws.StringValue(in.AccountName); name {
	case "a":
		s = &orgs.CreateAccountStatus{
			AccountId:   aws.String("000000000001"),
			AccountName: in.AccountName,
			Id:          aws.String("1"),
			State:       orgs.CreateAccountStateSucceeded,
		}
	case "b":
		s = &orgs.CreateAccountStatus{
			Id:    aws.String("2"),
			State: orgs.CreateAccountStateInProgress,
		}
	case "c":
		s = &orgs.CreateAccountStatus{
			Id:    aws.String("3"),
			State: orgs.CreateAccountStateInProgress,
		}
	default:
		panic("invalid account name: " + name)
	}
	q.Data.(*orgs.CreateAccountOutput).CreateAccountStatus = s
}

func (testOrg) DescribeCreateAccountStatus(q *mock.Request, in *orgs.DescribeCreateAccountStatusInput) {
	var s *orgs.CreateAccountStatus
	switch id := aws.StringValue(in.CreateAccountRequestId); id {
	case "2":
		s = &orgs.CreateAccountStatus{
			AccountId:   aws.String("000000000002"),
			AccountName: aws.String("b"),
			Id:          aws.String("2"),
			State:       orgs.CreateAccountStateSucceeded,
		}
	case "3":
		s = &orgs.CreateAccountStatus{
			FailureReason: orgs.CreateAccountFailureReasonInternalFailure,
			Id:            aws.String("3"),
			State:         orgs.CreateAccountStateFailed,
		}
	default:
		panic("invalid request id: " + id)
	}
	q.Data.(*orgs.DescribeCreateAccountStatusOutput).CreateAccountStatus = s
}

func (testOrg) DescribeAccount(q *mock.Request, in *orgs.DescribeAccountInput) {
	id := aws.StringValue(in.AccountId)
	ac := &orgs.Account{
		Arn:             aws.String("arn:aws:organizations::000000000000:account/o-test/" + id),
		Email:           aws.String("test@example.com"),
		Id:              in.AccountId,
		JoinedMethod:    orgs.AccountJoinedMethodCreated,
		JoinedTimestamp: aws.Time(time.Unix(1, 0)),
		Status:          orgs.AccountStatusActive,
	}
	switch id {
	case "000000000001":
		ac.Name = aws.String("a")
	case "000000000002":
		ac.Name = aws.String("b")
	default:
		panic("invalid account id: " + id)
	}
	q.Data.(*orgs.DescribeAccountOutput).Account = ac
}
