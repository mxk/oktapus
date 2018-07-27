package cmd

import (
	"testing"
	"time"

	"github.com/LuminalHQ/cloudcover/oktapus/mock"
	"github.com/LuminalHQ/cloudcover/x/fast"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreds(t *testing.T) {
	now := fast.MockTime(fast.Time())
	exp := expTime{now.Add(time.Hour - time.Minute).Truncate(time.Second)}
	defer fast.MockTime(time.Time{})

	ctx := newCtx()
	cmd := credsCmd{Spec: "test1,test2"}

	// Get temporary creds
	out, err := cmd.Call(ctx)
	require.NoError(t, err)
	want := []*credsOutput{{
		AccountID:       "000000000001",
		Name:            "test1",
		Expires:         exp,
		AccessKeyID:     mock.AccessKeyID,
		SecretAccessKey: mock.SecretAccessKey,
		SessionToken:    mock.AssumedRoleARN("1", "user@example.com", "user@example.com"),
	}, {
		AccountID:       "000000000002",
		Name:            "test2",
		Expires:         exp,
		AccessKeyID:     mock.AccessKeyID,
		SecretAccessKey: mock.SecretAccessKey,
		SessionToken:    mock.AssumedRoleARN("2", "user@example.com", "user@example.com"),
	}}
	assert.Equal(t, want, out)

	// Create new user
	cmd.User = "creds_user"
	cmd.Spec = "test1"
	out, err = cmd.Call(ctx)
	require.NoError(t, err)
	want = []*credsOutput{{
		AccountID:       "000000000001",
		Name:            "test1",
		AccessKeyID:     mock.AccessKeyID,
		SecretAccessKey: mock.SecretAccessKey,
	}}
	assert.Equal(t, want, out)

	// Create second key
	out, err = cmd.Call(ctx)
	require.NoError(t, err)
	assert.Equal(t, want, out)

	// Path must match
	cmd.User = "path/creds_user"
	out, err = cmd.Call(ctx)
	require.NoError(t, err)
	want = []*credsOutput{{
		AccountID: "000000000001",
		Name:      "test1",
		Error:     "user already exists under a different path",
	}}
	assert.Equal(t, want, out)
}