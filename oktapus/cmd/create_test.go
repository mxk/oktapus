package cmd

import (
	"testing"

	"github.com/LuminalHQ/cloudcover/oktapus/internal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreate(t *testing.T) {
	internal.NoSleep(true)
	defer internal.NoSleep(false)

	cmd := createCmd{
		Num:      3,
		EmailTpl: "test{}@example.com",
		NameTpl:  "test{00}",
	}
	ctx := newCtx()
	out, err := cmd.Call(ctx)
	require.NoError(t, err)
	want := []*newAccountsOutput{
		{Name: "test04", Email: "test4@example.com"},
		{Name: "test05", Email: "test5@example.com"},
		{Name: "test06", Email: "test6@example.com"},
	}
	assert.Equal(t, want, out)

	cmd.Exec = true
	cmd.Num = 1
	out, err = cmd.Call(ctx)
	require.NoError(t, err)
	results := []*resultsOutput{{
		AccountID: "000000000004",
		Name:      "test04",
		Result:    "OK",
	}}
	assert.Equal(t, results, out)
}
