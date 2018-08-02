package account

import (
	"io/ioutil"
	"os"
	"strings"
	"testing"

	"github.com/LuminalHQ/cloudcover/x/awsmock"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/awserr"
	"github.com/aws/aws-sdk-go-v2/aws/endpoints"
	orgs "github.com/aws/aws-sdk-go-v2/service/organizations"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDirectory(t *testing.T) {
	cfg := awsmock.Config(func(q *aws.Request) {
		switch out := q.Data.(type) {
		case *sts.GetCallerIdentityOutput:
			*out = sts.GetCallerIdentityOutput{
				Account: aws.String("000000000000"),
			}
		case *orgs.DescribeOrganizationOutput:
			out.Organization = &orgs.Organization{
				MasterAccountId: aws.String("000000000000"),
			}
		case *orgs.ListAccountsOutput:
			out.Accounts = []orgs.Account{
				{Id: aws.String("000000000000"), Name: aws.String("master")},
				{Id: aws.String("000000000001"), Name: aws.String("test1")},
			}
		default:
			panic("unsupported api: " + q.Operation.Name)
		}
	})
	d := NewDirectory(&cfg)
	require.NoError(t, d.Refresh())
	assert.Equal(t, Org{MasterID: "000000000000"}, d.Org())
	want := []*Info{
		{ID: "000000000000", Name: "master"},
		{ID: "000000000001", Name: "test1"},
	}
	assert.Equal(t, want, d.Accounts())

	// Aliases
	tmp, err := ioutil.TempFile("", "account_test.")
	require.NoError(t, err)
	defer func() {
		tmp.Close()
		os.Remove(tmp.Name())
	}()
	tmp.WriteString(strings.Join([]string{
		"# comment",
		"   ",
		"aws  000000000000  main",
		"aws\t000000000002\ttest2",
		"aws-us-gov 000000000003 test3",
		"",
	}, "\n"))
	require.NoError(t, tmp.Close())
	require.NoError(t, d.LoadAliases(tmp.Name()))

	want = []*Info{
		{ID: "000000000000", Name: "master", Alias: "main"},
		{ID: "000000000001", Name: "test1"},
		{ID: "000000000002", Alias: "test2"},
	}
	assert.Equal(t, want, d.Accounts())
	assert.Equal(t, "main", want[0].DisplayName())
	assert.Equal(t, "test1", want[1].DisplayName())
	assert.Equal(t, "test2", want[2].DisplayName())

	cfg.Region = endpoints.UsGovWest1RegionID
	d = NewDirectory(&cfg)
	require.NoError(t, d.LoadAliases(tmp.Name()))
	assert.Equal(t, []*Info{{ID: "000000000003", Alias: "test3"}}, d.Accounts())

	assert.Equal(t, &Info{ID: "000000000004"}, d.SetAlias("000000000004", ""))
	want = []*Info{
		{ID: "000000000003", Alias: "test3"},
		{ID: "000000000004"},
	}
	assert.Equal(t, want, d.Accounts())

	// Errors
	assert.Equal(t, ErrNoOrg, d.Refresh())

	cfg = awsmock.Config(func(q *aws.Request) {
		switch out := q.Data.(type) {
		case *sts.GetCallerIdentityOutput:
			*out = sts.GetCallerIdentityOutput{
				Account: aws.String("000000000000"),
			}
		case *orgs.DescribeOrganizationOutput:
			q.Error = awserr.New(orgs.ErrCodeAWSOrganizationsNotInUseException, "", nil)
		default:
			panic("unsupported api: " + q.Operation.Name)
		}
	})
	d = NewDirectory(&cfg)
	assert.Equal(t, ErrNoOrg, d.Init())
	assert.Equal(t, ErrNoOrg, d.Refresh())

	cfg = awsmock.Config(func(q *aws.Request) {
		switch out := q.Data.(type) {
		case *sts.GetCallerIdentityOutput:
			*out = sts.GetCallerIdentityOutput{
				Account: aws.String("000000000001"),
			}
		case *orgs.DescribeOrganizationOutput:
			out.Organization = &orgs.Organization{
				MasterAccountId: aws.String("000000000000"),
			}
		default:
			panic("unsupported api: " + q.Operation.Name)
		}
	})
	assert.Equal(t, ErrNotMaster, NewDirectory(&cfg).Refresh())
}
