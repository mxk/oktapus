package cmd

import (
	"testing"
	"time"

	"github.com/mxk/cloudcover/oktapus/mock"
	"github.com/mxk/cloudcover/x/fast"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreds(t *testing.T) {
	now := fast.MockTime(fast.Time())
	exp := expTime{now.Add(time.Hour)}
	defer fast.MockTime(time.Time{})

	ctx, w := mockOrg(mock.Ctx, "test1", "test2")
	cmd := credsCli.New().(*credsCmd)
	cmd.Spec = "test1,test2"

	// Get temporary creds
	out, err := cmd.Run(ctx)
	require.NoError(t, err)
	want := []*credsOutput{{
		Account:         "000000000001",
		Name:            "test1",
		Expires:         exp,
		AccessKeyID:     mock.AccessKeyID,
		SecretAccessKey: mock.SecretAccessKey,
		SessionToken:    w.SessionToken("1", "alice", ""),
	}, {
		Account:         "000000000002",
		Name:            "test2",
		Expires:         exp,
		AccessKeyID:     mock.AccessKeyID,
		SecretAccessKey: mock.SecretAccessKey,
		SessionToken:    w.SessionToken("2", "alice", ""),
	}}
	assert.Equal(t, want, out)

	// Create new user
	cmd.User = "creds_user"
	cmd.Spec = "test1"
	out, err = cmd.Run(ctx)
	require.NoError(t, err)
	want = []*credsOutput{{
		Account:         "000000000001",
		Name:            "test1",
		AccessKeyID:     mock.AccessKeyID,
		SecretAccessKey: mock.SecretAccessKey,
	}}
	assert.Equal(t, want, out)

	// Create second key
	out, err = cmd.Run(ctx)
	require.NoError(t, err)
	assert.Equal(t, want, out)

	// Path must match
	cmd.User = "path/creds_user"
	out, err = cmd.Run(ctx)
	require.NoError(t, err)
	want = []*credsOutput{{
		Account: "000000000001",
		Name:    "test1",
		Error:   "user path mismatch",
	}}
	assert.Equal(t, want, out)
}
