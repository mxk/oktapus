package mock

import (
	"testing"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/iam"
	"github.com/aws/aws-sdk-go/service/organizations"
	"github.com/aws/aws-sdk-go/service/sts"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOrg(t *testing.T) {
	out, err := organizations.New(NewSession()).DescribeOrganization(nil)
	require.NoError(t, err)
	assert.Equal(t, "000000000000", aws.StringValue(out.Organization.MasterAccountId))
}

func TestSTS(t *testing.T) {
	out, err := sts.New(NewSession()).GetCallerIdentity(nil)
	require.NoError(t, err)
	assert.Equal(t, "000000000000", aws.StringValue(out.Account))
}

func TestIAM(t *testing.T) {
	out, err := iam.New(NewSession()).CreateRole(new(iam.CreateRoleInput).
		SetAssumeRolePolicyDocument("{}").
		SetRoleName("testrole"))
	require.NoError(t, err)
	assert.Equal(t, "testrole", aws.StringValue(out.Role.RoleName))
}

func TestFind(t *testing.T) {
	s := NewSession()
	org := s.OrgsRouter()
	require.NotNil(t, org)
	require.NotNil(t, s.STSRouter())

	ac := org.Account("")
	require.NotNil(t, ac.RoleRouter())
	require.NotNil(t, ac.UserRouter())
}

func TestAccountID(t *testing.T) {
	assert.Equal(t, "123456789012", AccountID("123456789012"))
	assert.Equal(t, "000000000000", AccountID(""))
	assert.Equal(t, "000000000123", AccountID("123"))
	assert.Equal(t, "123456789012", AccountID(UserARN("123456789012", "user")))
}
