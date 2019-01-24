package account

import (
	"io/ioutil"
	"os"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/endpoints"
	orgs "github.com/aws/aws-sdk-go-v2/service/organizations"
	"github.com/mxk/go-cloud/aws/awsmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsID(t *testing.T) {
	assert.False(t, IsID(""))
	assert.False(t, IsID("123456789/12"))
	assert.False(t, IsID("123456789:12"))
	assert.True(t, IsID("123456789012"))
}

func TestIsErrorNoOrg(t *testing.T) {
	assert.False(t, IsErrorNoOrg(nil))
	assert.True(t, IsErrorNoOrg(errNoOrg))
}

func TestLoadAliases(t *testing.T) {
	tmp, err := ioutil.TempFile("", "account_test.")
	require.NoError(t, err)
	defer func() {
		tmp.Close()
		assert.NoError(t, os.Remove(tmp.Name()))
	}()
	tmp.WriteString(strings.Join([]string{
		"# comment",
		"   ",
		"aws  000000000000  main",
		"aws\t000000000001\ttest1",
		"aws-us-gov 000000000002 test2",
		"",
	}, "\n"))
	require.NoError(t, tmp.Close())

	have, err := LoadAliases(tmp.Name(), endpoints.AwsPartitionID)
	assert.NoError(t, err)
	want := map[string]string{
		"000000000000": "main",
		"000000000001": "test1",
	}
	assert.Equal(t, want, have)

	have, err = LoadAliases(tmp.Name(), endpoints.AwsUsGovPartitionID)
	assert.NoError(t, err)
	want = map[string]string{"000000000002": "test2"}
	assert.Equal(t, want, have)

	have, err = LoadAliases(tmp.Name(), endpoints.AwsCnPartitionID)
	assert.NoError(t, err)
	assert.Nil(t, have)
}

func TestDirectory(t *testing.T) {
	cfg := awsmock.Config(func(q *aws.Request) {
		switch out := q.Data.(type) {
		case *orgs.DescribeOrganizationOutput:
			out.Organization = &orgs.Organization{
				MasterAccountId: aws.String("000000000000"),
			}
		case *orgs.ListAccountsOutput:
			if q.Params.(*orgs.ListAccountsInput).NextToken == nil {
				out.Accounts = []orgs.Account{
					{Id: aws.String("000000000000"), Name: aws.String("master")},
				}
				out.NextToken = aws.String("1")
			} else {
				out.Accounts = []orgs.Account{
					{Id: aws.String("000000000001"), Name: aws.String("test1")},
				}
			}
		default:
			panic("unsupported api: " + q.Operation.Name)
		}
	})
	d := Directory{Client: NewClient(&cfg)}
	require.NoError(t, d.Init())
	assert.Equal(t, Org{MasterID: "000000000000"}, d.Org)

	require.NoError(t, d.Refresh())
	want := map[string]*Info{
		"000000000000": {ID: "000000000000", Name: "master"},
		"000000000001": {ID: "000000000001", Name: "test1"},
	}
	assert.Equal(t, want, d.Accounts)

	d.Client.Config.Region = endpoints.UsGovWest1RegionID
	assert.Equal(t, errNoOrg, d.Init())
	assert.Equal(t, errNoOrg, d.Refresh())
}
