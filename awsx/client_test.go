package awsx

import (
	"crypto/hmac"
	"crypto/sha512"
	"encoding/hex"
	"sort"
	"testing"
	"time"

	"github.com/LuminalHQ/oktapus/mock"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/service/sts"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClientConnect(t *testing.T) {
	s := mock.NewSession()
	c := NewClient(s, "")
	assert.Equal(t, s, c.ConfigProvider())
	assert.Empty(t, c.Ident().AccountID)
	assert.Empty(t, c.OrgInfo().MasterAccountID)

	require.NoError(t, c.Connect())
	assert.Error(t, c.Connect())
	assert.Equal(t, "000000000000", c.Ident().AccountID)
	assert.Equal(t, "000000000000", c.OrgInfo().MasterAccountID)
	assert.NotNil(t, c.OrgsClient())
}

func TestClientCommonRole(t *testing.T) {
	// Assumed role
	s := mock.NewSession()
	c := NewClient(s, "")
	require.NoError(t, c.Connect())
	assert.Equal(t, "user@example.com", c.commonRoleName)

	// IAM user
	rtr := mock.NewDataTypeRouter(&sts.GetCallerIdentityOutput{
		Account: aws.String("000000000000"),
		Arn:     aws.String("arn:aws:iam::123456789012:user/path/TestUser"),
		UserId:  aws.String("AKIAI44QH8DHBEXAMPLE"),
	})
	s.ChainRouter = append(s.ChainRouter, rtr)
	c = NewClient(s, "")
	require.NoError(t, c.Connect())
	assert.Equal(t, "TestUser", c.commonRoleName)

	// Root (shouldn't be used, but test anyway)
	rtr.Set(&sts.GetCallerIdentityOutput{
		Account: aws.String("000000000000"),
		Arn:     aws.String("arn:aws:iam::000000000000:root"),
		UserId:  aws.String("000000000000"),
	}, nil)
	c = NewClient(s, "")
	require.NoError(t, c.Connect())
	assert.Equal(t, "OrganizationAccountAccessRole", c.commonRoleName)
}

func TestClientRefresh(t *testing.T) {
	s := mock.NewSession()
	acs := s.OrgsRouter().AllAccounts()
	want := make([]*Account, len(acs))
	for i := range want {
		want[i] = &Account{ID: aws.StringValue(acs[i].Id)}
		want[i].set(acs[i])
	}
	assert.Panics(t, func() {
		new(Account).set(acs[0])
	})
	c := NewClient(s, "")
	require.NoError(t, c.Connect())
	assert.True(t, c.IsMaster())
	require.NoError(t, c.Refresh())
	assert.Equal(t, want, sortByID(c.Accounts()))
}

func TestClientRefreshProxy(t *testing.T) {
	s := mock.NewSession()
	s.STSRouter()[""] = mock.NewSTSRouter("1")[""]
	c := NewClient(s, "MasterRole")
	require.NoError(t, c.Connect())
	assert.False(t, c.IsMaster())
	assert.Equal(t, "000000000001", c.Ident().AccountID)

	cr := c.proxyCreds()
	require.NotNil(t, cr.ExternalId)
	h := hmac.New(sha512.New512_256, []byte("o-test"))
	h.Write([]byte("oktapus:000000000000:master@example.com"))
	require.Equal(t, hex.EncodeToString(h.Sum(nil)), *cr.ExternalId)

	require.NoError(t, c.Refresh())
	assert.Len(t, c.Accounts(), 4)
}

func TestClientEncodeDecode(t *testing.T) {
	s := mock.NewSession()
	creds := &StaticCreds{
		Value: credentials.Value{
			AccessKeyID:     "ID",
			SecretAccessKey: "SECRET",
		},
		Exp: time.Now().Add(time.Minute).Truncate(time.Second),
	}
	c := NewClient(s, "")
	c.GatewayCreds = creds
	require.NoError(t, c.Connect())
	require.NoError(t, c.Refresh())
	want := sortByID(c.Accounts())
	state, err := c.GobEncode()
	require.NoError(t, err)

	c = NewClient(s, "")
	require.NoError(t, c.GobDecode(state))
	assert.Equal(t, creds.Save(), c.GatewayCreds.Save())
	require.NoError(t, c.Connect())
	assert.Equal(t, want, sortByID(c.Accounts()))
}

func TestClientCreds(t *testing.T) {
	c := NewClient(mock.NewSession(), "")
	require.NoError(t, c.Connect())
	require.NoError(t, c.Refresh())
	cp := c.CredsProvider("111111111111").(*AssumeRoleCreds)
	assert.Equal(t, "arn:aws:iam::111111111111:role/user@example.com", *cp.RoleArn)
	assert.Equal(t, "user@example.com", *cp.RoleSessionName)
}

func sortByID(acs []*Account) []*Account {
	sort.Slice(acs, func(i, j int) bool {
		return acs[i].ID < acs[j].ID
	})
	return acs
}
