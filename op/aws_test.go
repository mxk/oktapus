package op

import (
	"testing"
	"time"

	"github.com/LuminalHQ/oktapus/mock"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/iam"
	orgs "github.com/aws/aws-sdk-go/service/organizations"
	orgsiface "github.com/aws/aws-sdk-go/service/organizations/organizationsiface"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSplitPath(t *testing.T) {
	tests := []*struct{ in, path, name string }{
		{in: "", path: "/", name: ""},
		{in: "a", path: "/", name: "a"},
		{in: "/", path: "/", name: ""},
		{in: "a/", path: "/a/", name: ""},
		{in: "/a", path: "/", name: "a"},
		{in: "/a/", path: "/a/", name: ""},
		{in: "a/b", path: "/a/", name: "b"},
		{in: "/a/b", path: "/a/", name: "b"},
	}
	for _, test := range tests {
		path, name := SplitPath(test.in)
		assert.Equal(t, test.path, path, "in=%q", test.in)
		assert.Equal(t, test.name, name, "in=%q", test.in)
	}
}

func TestCreateAccounts(t *testing.T) {
	sleep = func(d time.Duration) {}
	ch := make(chan *orgs.CreateAccountInput)
	go func() {
		defer close(ch)
		ch <- &orgs.CreateAccountInput{
			AccountName: aws.String("a"),
			Email:       aws.String("test@example.com"),
		}
		ch <- &orgs.CreateAccountInput{
			AccountName: aws.String("b"),
			Email:       aws.String("test@example.com"),
		}
		ch <- &orgs.CreateAccountInput{
			AccountName: aws.String("c"),
			Email:       aws.String("test@example.com"),
		}
	}()
	var a, b, c *orgs.Account
	for r := range CreateAccounts(testOrg{}, ch) {
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

func TestDelTmpUsers(t *testing.T) {
	sess := mock.NewSession(true)
	c := iam.New(sess)
	require.NoError(t, DelTmpUsers(c))

	var org mock.OrgRouter
	sess.ChainRouter.GetType(&org)
	var r mock.UserRouter
	org.GetRouter("").GetType(&r)

	r["a"] = &mock.User{User: iam.User{
		Arn:      aws.String(mock.UserARN("", "a")),
		Path:     aws.String("/"),
		UserName: aws.String("a"),
	}}
	r["b"] = &mock.User{
		User: iam.User{
			Arn:      aws.String(mock.UserARN("", "b")),
			Path:     aws.String(TmpIAMPath),
			UserName: aws.String("b"),
		},
		AttachedPolicies: []*iam.AttachedPolicy{{
			PolicyArn:  aws.String(mock.PolicyARN("", "TestPolicy")),
			PolicyName: aws.String("TestPolicy"),
		}},
		AccessKeys: []*iam.AccessKeyMetadata{{
			AccessKeyId: aws.String("AKIAIOSFODNN7EXAMPLE"),
			Status:      aws.String(iam.StatusTypeActive),
			UserName:    aws.String("b"),
		}},
	}

	require.NoError(t, DelTmpUsers(c))
	assert.Contains(t, r, "a")
	assert.NotContains(t, r, "b")
}

func TestDelTmpRoles(t *testing.T) {
	sess := mock.NewSession(true)
	c := iam.New(sess)
	require.NoError(t, DelTmpRoles(c))

	var org mock.OrgRouter
	sess.ChainRouter.GetType(&org)
	var r mock.RoleRouter
	org.GetRouter("").GetType(&r)

	r["a"] = &mock.Role{Role: iam.Role{
		Arn:      aws.String(mock.RoleARN("", "a")),
		Path:     aws.String("/"),
		RoleName: aws.String("a"),
	}}
	r["b"] = &mock.Role{
		Role: iam.Role{
			Arn:      aws.String(mock.RoleARN("", "b")),
			Path:     aws.String(TmpIAMPath),
			RoleName: aws.String("b"),
		},
		AttachedPolicies: []*iam.AttachedPolicy{{
			PolicyArn:  aws.String(mock.PolicyARN("", "TestPolicy1")),
			PolicyName: aws.String("TestPolicy1"),
		}, {
			PolicyArn:  aws.String(mock.PolicyARN("", "TestPolicy2")),
			PolicyName: aws.String("TestPolicy2"),
		}},
	}

	require.NoError(t, DelTmpRoles(c))
	assert.Contains(t, r, "a")
	assert.NotContains(t, r, "b")
}

type testOrg struct{ orgsiface.OrganizationsAPI }

func (testOrg) CreateAccount(in *orgs.CreateAccountInput) (*orgs.CreateAccountOutput, error) {
	switch name := aws.StringValue(in.AccountName); name {
	case "a":
		return &orgs.CreateAccountOutput{CreateAccountStatus: &orgs.CreateAccountStatus{
			AccountId:   aws.String("000000000001"),
			AccountName: in.AccountName,
			Id:          aws.String("1"),
			State:       aws.String(orgs.CreateAccountStateSucceeded),
		}}, nil
	case "b":
		return &orgs.CreateAccountOutput{CreateAccountStatus: &orgs.CreateAccountStatus{
			Id:    aws.String("2"),
			State: aws.String(orgs.CreateAccountStateInProgress),
		}}, nil
	case "c":
		return &orgs.CreateAccountOutput{CreateAccountStatus: &orgs.CreateAccountStatus{
			Id:    aws.String("3"),
			State: aws.String(orgs.CreateAccountStateInProgress),
		}}, nil
	default:
		panic("invalid account name: " + name)
	}
}

func (testOrg) DescribeCreateAccountStatus(in *orgs.DescribeCreateAccountStatusInput) (*orgs.DescribeCreateAccountStatusOutput, error) {
	switch id := aws.StringValue(in.CreateAccountRequestId); id {
	case "2":
		return &orgs.DescribeCreateAccountStatusOutput{CreateAccountStatus: &orgs.CreateAccountStatus{
			AccountId:   aws.String("000000000002"),
			AccountName: aws.String("b"),
			Id:          aws.String("2"),
			State:       aws.String(orgs.CreateAccountStateSucceeded),
		}}, nil
	case "3":
		return &orgs.DescribeCreateAccountStatusOutput{CreateAccountStatus: &orgs.CreateAccountStatus{
			FailureReason: aws.String("INTERNAL_FAILURE"),
			Id:            aws.String("3"),
			State:         aws.String(orgs.CreateAccountStateFailed),
		}}, nil
	default:
		panic("invalid request id: " + id)
	}
}

func (testOrg) DescribeAccount(in *orgs.DescribeAccountInput) (*orgs.DescribeAccountOutput, error) {
	id := aws.StringValue(in.AccountId)
	ac := &orgs.Account{
		Arn:             aws.String("arn:aws:organizations::000000000000:account/o-test/" + id),
		Email:           aws.String("test@example.com"),
		Id:              in.AccountId,
		JoinedMethod:    aws.String(orgs.AccountJoinedMethodCreated),
		JoinedTimestamp: aws.Time(time.Unix(1, 0)),
		Status:          aws.String(orgs.AccountStatusActive),
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
