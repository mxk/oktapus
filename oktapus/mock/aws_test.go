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

func TestAccountID(t *testing.T) {
	assert.Equal(t, "000000000000", AccountID(""))
	assert.Equal(t, "000000000123", AccountID("123"))
	assert.Equal(t, "123456789012", AccountID("123456789012"))
	assert.Panics(t, func() { AccountID("x") })
}

func TestIAM(t *testing.T) {
	w := NewAWS(Ctx, RoleRouter{})
	out, err := iam.New(w.Cfg).CreateRoleRequest(&iam.CreateRoleInput{
		AssumeRolePolicyDocument: aws.String("{}"),
		RoleName:                 aws.String("testrole"),
	}).Send()
	require.NoError(t, err)
	assert.Equal(t, "testrole", aws.StringValue(out.Role.RoleName))
}

func TestOrg(t *testing.T) {
	w := NewAWS(Ctx, NewOrg(Ctx, "master"))
	out, err := orgs.New(w.Cfg).DescribeOrganizationRequest(nil).Send()
	require.NoError(t, err)
	assert.Equal(t, "000000000000", aws.StringValue(out.Organization.MasterAccountId))
}

func TestSTS(t *testing.T) {
	w := NewAWS(Ctx)
	out, err := sts.New(w.Cfg).GetCallerIdentityRequest(nil).Send()
	require.NoError(t, err)
	assert.Equal(t, "000000000000", aws.StringValue(out.Account))
}

func TestFind(t *testing.T) {
	w := NewAWS(Ctx)
	r := w.Root()
	assert.NotNil(t, r.STSRouter())
	assert.NotNil(t, r.DataTypeRouter())
	assert.NotNil(t, r.UserRouter())

	rr := r.RoleRouter()
	assert.NotNil(t, rr)
	rr["test"] = nil
	assert.Equal(t, rr, r.RoleRouter())

	assert.Nil(t, w.Root().OrgRouter())
	r.Add(NewOrg(w.Ctx, "master"))
	assert.NotNil(t, w.Root().OrgRouter())
}
