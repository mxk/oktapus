package mock

import (
	"testing"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/organizations"
	"github.com/aws/aws-sdk-go/service/sts"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSession(t *testing.T) {
	sess := NewSession(true)
	id, err := sts.New(sess).GetCallerIdentity(nil)
	require.NoError(t, err)
	assert.Equal(t, "000000000000", aws.StringValue(id.Account))

	out, err := organizations.New(sess).DescribeOrganization(nil)
	require.NoError(t, err)
	assert.Equal(t, "000000000000", aws.StringValue(out.Organization.MasterAccountId))
}
