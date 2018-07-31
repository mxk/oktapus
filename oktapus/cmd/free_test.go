package cmd

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFree(t *testing.T) {
	cmd := freeCmd{Spec: "test1,test2,test3"}
	ctx, _ := newCtx("1", "2")

	acs, err := ctx.Accounts("test1")
	require.NoError(t, err)
	acs[0].Ctl.Owner = "user@example.com"
	acs.Save()

	out, err := cmd.Call(ctx)
	require.NoError(t, err)
	want := []*resultsOutput{{
		AccountID: "000000000001",
		Name:      "test1",
		Result:    "OK",
	}}
	assert.Equal(t, want, out)
}
