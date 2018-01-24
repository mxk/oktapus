package awsgw

import (
	"sort"
	"testing"
	"time"

	"github.com/LuminalHQ/oktapus/mock"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	orgs "github.com/aws/aws-sdk-go/service/organizations"
	"github.com/aws/aws-sdk-go/service/sts"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// OrgRouter contains API responses for a mock AWS organization.
var OrgRouter = mock.NewDataTypeRouter(
	&sts.GetCallerIdentityOutput{
		Account: aws.String("000000000000"),
		Arn:     aws.String("arn:aws:sts::000000000000:assumed-role/TestRole/TestSession"),
		UserId:  aws.String("AKIAI44QH8DHBEXAMPLE:user@example.com"),
	},
	&orgs.DescribeOrganizationOutput{
		Organization: &orgs.Organization{
			Arn:                aws.String("arn:aws:organizations::000000000000:organization/o-test"),
			FeatureSet:         aws.String(orgs.OrganizationFeatureSetAll),
			Id:                 aws.String("o-test"),
			MasterAccountArn:   aws.String("arn:aws:organizations::000000000000:account/o-test/000000000000"),
			MasterAccountEmail: aws.String("master@example.com"),
			MasterAccountId:    aws.String("000000000000"),
		},
	},
	&orgs.ListAccountsOutput{
		Accounts: []*orgs.Account{{
			Arn:             aws.String("arn:aws:organizations::000000000000:account/o-test/000000000000"),
			Email:           aws.String("master@example.com"),
			Id:              aws.String("000000000000"),
			JoinedMethod:    aws.String(orgs.AccountJoinedMethodInvited),
			JoinedTimestamp: aws.Time(time.Unix(0, 0)),
			Name:            aws.String("master"),
			Status:          aws.String(orgs.AccountStatusActive),
		}, {
			Arn:             aws.String("arn:aws:organizations::000000000000:account/o-test/111111111111"),
			Email:           aws.String("test1@example.com"),
			Id:              aws.String("111111111111"),
			JoinedMethod:    aws.String(orgs.AccountJoinedMethodCreated),
			JoinedTimestamp: aws.Time(time.Unix(1, 0)),
			Name:            aws.String("test1"),
			Status:          aws.String(orgs.AccountStatusActive),
		}, {
			Arn:             aws.String("arn:aws:organizations::000000000000:account/o-test/222222222222"),
			Email:           aws.String("test2@example.com"),
			Id:              aws.String("222222222222"),
			JoinedMethod:    aws.String(orgs.AccountJoinedMethodCreated),
			JoinedTimestamp: aws.Time(time.Unix(2, 0)),
			Name:            aws.String("test2"),
			Status:          aws.String(orgs.AccountStatusSuspended),
		}},
	},
)

func TestClientConnect(t *testing.T) {
	sess := mockSession()
	c := NewClient(sess)
	assert.Equal(t, sess, c.ConfigProvider())
	assert.Empty(t, c.Ident().AccountID)
	assert.Empty(t, c.OrgInfo().MasterAccountID)

	require.NoError(t, c.Connect())
	assert.Error(t, c.Connect())
	assert.Equal(t, "000000000000", c.Ident().AccountID)
	assert.Equal(t, "000000000000", c.OrgInfo().MasterAccountID)
	assert.NotNil(t, c.OrgClient())
}

func TestClientCommonRole(t *testing.T) {
	// Assumed role
	sess := mockSession()
	c := NewClient(sess)
	require.NoError(t, c.Connect())
	assert.Equal(t, "user@example.com", c.CommonRole)

	// IAM user
	rtr := mock.NewDataTypeRouter(&sts.GetCallerIdentityOutput{
		Account: aws.String("000000000000"),
		Arn:     aws.String("arn:aws:iam::123456789012:user/path/TestUser"),
		UserId:  aws.String("AKIAI44QH8DHBEXAMPLE"),
	})
	sess.Add(rtr)
	c = NewClient(sess)
	require.NoError(t, c.Connect())
	assert.Equal(t, "TestUser", c.CommonRole)

	// Root (shouldn't be used, but test anyway)
	rtr.Set(&sts.GetCallerIdentityOutput{
		Account: aws.String("000000000000"),
		Arn:     aws.String("arn:aws:iam::000000000000:root"),
		UserId:  aws.String("000000000000"),
	}, nil)
	c = NewClient(sess)
	require.NoError(t, c.Connect())
	assert.Equal(t, "OrganizationAccountAccessRole", c.CommonRole)
}

func TestClientRefresh(t *testing.T) {
	out := new(orgs.ListAccountsOutput)
	OrgRouter.Get(out)
	want := make([]*Account, len(out.Accounts))
	for i := range want {
		want[i] = &Account{ID: aws.StringValue(out.Accounts[i].Id)}
		want[i].set(out.Accounts[i])
	}
	assert.Panics(t, func() {
		new(Account).set(out.Accounts[0])
	})

	c := NewClient(mockSession())
	require.NoError(t, c.Connect())
	require.NoError(t, c.Refresh())
	assert.Equal(t, want, sortByID(c.Accounts()))
}

func TestClientState(t *testing.T) {
	sess := mockSession()
	creds := &StaticCreds{
		Value: credentials.Value{
			AccessKeyID:     "ID",
			SecretAccessKey: "SECRET",
			SessionToken:    "TOKEN",
		},
		Exp: time.Now().Add(time.Minute).Truncate(time.Second),
	}
	c := NewClient(sess)
	c.MasterCreds = creds
	require.NoError(t, c.Connect())
	require.NoError(t, c.Refresh())
	want := sortByID(c.Accounts())
	state, err := c.GobEncode()
	require.NoError(t, err)

	c = NewClient(sess)
	require.NoError(t, c.GobDecode(state))
	assert.Equal(t, creds.Save(), c.MasterCreds.Save())
	require.NoError(t, c.Connect())
	assert.Equal(t, want, sortByID(c.Accounts()))
}

func TestClientCreds(t *testing.T) {
	c := NewClient(mockSession())
	require.NoError(t, c.Connect())
	require.NoError(t, c.Refresh())
	cp := c.CredsProvider("111111111111").(*AssumeRoleCreds)
	assert.Equal(t, "arn:aws:iam::111111111111:role/user@example.com", *cp.RoleArn)
	assert.Equal(t, "user@example.com", *cp.RoleSessionName)
}

func mockSession() *mock.Session {
	sess := mock.NewSession()
	sess.Add(OrgRouter)
	return sess
}

func sortByID(acs []*Account) []*Account {
	sort.Slice(acs, func(i, j int) bool {
		return acs[i].ID < acs[j].ID
	})
	return acs
}
