package awsx

import (
	"crypto/hmac"
	"crypto/sha512"
	"encoding/hex"
	"sort"
	"testing"

	"github.com/LuminalHQ/cloudcover/oktapus/mock"
	"github.com/LuminalHQ/cloudcover/x/arn"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClientConnect(t *testing.T) {
	s := mock.NewSession()
	c := NewGateway(&s.Config)
	assert.Empty(t, c.Ident().Account)
	assert.Empty(t, c.Org().MasterID)

	require.NoError(t, c.Connect())
	assert.Error(t, c.Connect())
	assert.Equal(t, "000000000000", c.Ident().Account)
	assert.Equal(t, "000000000000", c.Org().MasterID)
	assert.NotNil(t, c.OrgsClient())
}

func TestClientRefresh(t *testing.T) {
	s := mock.NewSession()
	acs := s.OrgsRouter().AllAccounts()
	want := make([]*Account, len(acs))
	for i := range want {
		want[i] = &Account{ID: aws.StringValue(acs[i].Id)}
		want[i].Set(&acs[i])
	}
	assert.Panics(t, func() {
		new(Account).Set(&acs[0])
	})
	c := NewGateway(&s.Config)
	require.NoError(t, c.Connect())
	assert.True(t, c.IsMaster())
	require.NoError(t, c.Refresh())
	assert.Equal(t, want, sortByID(c.Accounts()))
}

func TestClientRefreshProxy(t *testing.T) {
	s := mock.NewSession()
	s.STSRouter()[""] = mock.NewSTSRouter("1")[""]
	c := NewGateway(&s.Config)
	c.MasterRole = arn.Base + "role/MasterRole"
	require.NoError(t, c.Connect())
	assert.False(t, c.IsMaster())
	assert.Equal(t, "000000000001", c.Ident().Account)
	require.NoError(t, c.Refresh())
	assert.Len(t, c.Accounts(), 4)
}

func TestClientCreds(t *testing.T) {
	c := NewGateway(&mock.NewSession().Config)
	require.NoError(t, c.Connect())
	require.NoError(t, c.Refresh())
	cp := c.CredsProvider("111111111111")
	assert.NotNil(t, cp)
}

func TestProxyExternalID(t *testing.T) {
	h := hmac.New(sha512.New512_256, []byte("o-test"))
	h.Write([]byte("oktapus:000000000000:master@example.com"))
	assert.Equal(t, hex.EncodeToString(h.Sum(nil)), ProxyExternalID(&Org{
		ID:          "o-test",
		MasterID:    "000000000000",
		MasterEmail: "master@example.com",
	}))
}

func sortByID(acs []*Account) []*Account {
	sort.Slice(acs, func(i, j int) bool {
		return acs[i].ID < acs[j].ID
	})
	return acs
}
