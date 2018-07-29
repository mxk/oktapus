package awsx

import (
	"testing"
	"time"

	"github.com/LuminalHQ/cloudcover/oktapus/mock"
	"github.com/LuminalHQ/cloudcover/x/fast"
	"github.com/aws/aws-sdk-go-v2/aws"
	orgs "github.com/aws/aws-sdk-go-v2/service/organizations"
	"github.com/stretchr/testify/assert"
)

func TestCreateAccounts(t *testing.T) {
	s := mock.NewSession()
	s.ChainRouter = append(s.ChainRouter[:0], testOrg{})
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
	for r := range CreateAccounts(orgs.New(s.Config), in) {
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

func (r testOrg) Route(q *aws.Request) bool {
	var out interface{}
	var err error
	switch in := q.Params.(type) {
	case *orgs.CreateAccountInput:
		out, err = r.createAccount(in)
	case *orgs.DescribeAccountInput:
		out, err = r.describeAccount(in)
	case *orgs.DescribeCreateAccountStatusInput:
		out, err = r.describeCreateAccountStatus(in)
	default:
		return false
	}
	q.Data = out
	q.Error = err
	return true
}

func (testOrg) createAccount(in *orgs.CreateAccountInput) (*orgs.CreateAccountOutput, error) {
	switch name := aws.StringValue(in.AccountName); name {
	case "a":
		return &orgs.CreateAccountOutput{CreateAccountStatus: &orgs.CreateAccountStatus{
			AccountId:   aws.String("000000000001"),
			AccountName: in.AccountName,
			Id:          aws.String("1"),
			State:       orgs.CreateAccountStateSucceeded,
		}}, nil
	case "b":
		return &orgs.CreateAccountOutput{CreateAccountStatus: &orgs.CreateAccountStatus{
			Id:    aws.String("2"),
			State: orgs.CreateAccountStateInProgress,
		}}, nil
	case "c":
		return &orgs.CreateAccountOutput{CreateAccountStatus: &orgs.CreateAccountStatus{
			Id:    aws.String("3"),
			State: orgs.CreateAccountStateInProgress,
		}}, nil
	default:
		panic("invalid account name: " + name)
	}
}

func (testOrg) describeCreateAccountStatus(in *orgs.DescribeCreateAccountStatusInput) (*orgs.DescribeCreateAccountStatusOutput, error) {
	switch id := aws.StringValue(in.CreateAccountRequestId); id {
	case "2":
		return &orgs.DescribeCreateAccountStatusOutput{CreateAccountStatus: &orgs.CreateAccountStatus{
			AccountId:   aws.String("000000000002"),
			AccountName: aws.String("b"),
			Id:          aws.String("2"),
			State:       orgs.CreateAccountStateSucceeded,
		}}, nil
	case "3":
		return &orgs.DescribeCreateAccountStatusOutput{CreateAccountStatus: &orgs.CreateAccountStatus{
			FailureReason: orgs.CreateAccountFailureReasonInternalFailure,
			Id:            aws.String("3"),
			State:         orgs.CreateAccountStateFailed,
		}}, nil
	default:
		panic("invalid request id: " + id)
	}
}

func (testOrg) describeAccount(in *orgs.DescribeAccountInput) (*orgs.DescribeAccountOutput, error) {
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
	return &orgs.DescribeAccountOutput{Account: ac}, nil
}
