package cmd

import (
	"testing"
	"time"

	"github.com/LuminalHQ/cloudcover/oktapus/internal"
	"github.com/LuminalHQ/cloudcover/oktapus/mock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAlloc(t *testing.T) {
	now := internal.Time()
	exp := expTime{now.Add(time.Hour - time.Minute).Truncate(time.Second)}
	internal.SetTime(now)
	defer internal.SetTime(time.Time{})

	internal.NoSleep(true)
	defer internal.NoSleep(false)

	cmd := allocCmd{Num: 1, Spec: "test1"}
	ctx := newCtx("1", "2", "3")
	out, err := cmd.Call(ctx)
	require.NoError(t, err)
	want := []*credsOutput{{
		AccountID:       "000000000001",
		Name:            "test1",
		Expires:         exp,
		AccessKeyID:     mock.AccessKeyID,
		SecretAccessKey: mock.SecretAccessKey,
		SessionToken:    mock.AssumedRoleARN("1", "user@example.com", "user@example.com"),
	}}
	assert.Equal(t, want, out)
}
