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
	sess := NewSession()
	out, err := organizations.New(sess).DescribeOrganization(nil)
	require.NoError(t, err)
	assert.Equal(t, "000000000000", aws.StringValue(out.Organization.MasterAccountId))
}

func TestSTS(t *testing.T) {
	sess := NewSession()
	out, err := sts.New(sess).GetCallerIdentity(nil)
	require.NoError(t, err)
	assert.Equal(t, "000000000000", aws.StringValue(out.Account))
}

func TestIAM(t *testing.T) {
	sess := NewSession()
	out, err := iam.New(sess).CreateRole(new(iam.CreateRoleInput).
		SetAssumeRolePolicyDocument("{}").
		SetRoleName("testrole"))
	require.NoError(t, err)
	assert.Equal(t, "testrole", aws.StringValue(out.Role.RoleName))
}

func TestAccountID(t *testing.T) {
	assert.Equal(t, "123456789012", AccountID("123456789012"))
	assert.Equal(t, "000000000000", AccountID(""))
	assert.Equal(t, "000000000123", AccountID("123"))
	assert.Equal(t, "123456789012", AccountID(UserARN("123456789012", "user")))
}
