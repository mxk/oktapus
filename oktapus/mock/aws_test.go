package mock

import (
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	orgs "github.com/aws/aws-sdk-go-v2/service/organizations"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOrg(t *testing.T) {
	out, err := orgs.New(NewAWS("").Cfg).DescribeOrganizationRequest(nil).Send()
	require.NoError(t, err)
	assert.Equal(t, "000000000000", aws.StringValue(out.Organization.MasterAccountId))
}

func TestSTS(t *testing.T) {
	out, err := sts.New(NewAWS("").Cfg).GetCallerIdentityRequest(nil).Send()
	require.NoError(t, err)
	assert.Equal(t, "000000000000", aws.StringValue(out.Account))
}

func TestIAM(t *testing.T) {
	out, err := iam.New(NewAWS("").Cfg).CreateRoleRequest(&iam.CreateRoleInput{
		AssumeRolePolicyDocument: aws.String("{}"),
		RoleName:                 aws.String("testrole"),
	}).Send()
	require.NoError(t, err)
	assert.Equal(t, "testrole", aws.StringValue(out.Role.RoleName))
}

func TestFind(t *testing.T) {
	w := NewAWS("")
	ar := w.AccountRouter()
	require.NotNil(t, ar)
	require.NotNil(t, w.STSRouter())

	ac := ar.Get("")
	require.NotNil(t, ac.RoleRouter())
	require.NotNil(t, ac.UserRouter())
}

func TestAccountID(t *testing.T) {
	assert.Equal(t, "000000000000", AccountID(""))
	assert.Equal(t, "000000000123", AccountID("123"))
	assert.Equal(t, "123456789012", AccountID("123456789012"))
}
