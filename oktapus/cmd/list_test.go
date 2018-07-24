package cmd

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestList(t *testing.T) {
	cmd := listCmd{Refresh: true}
	ctx := newCtx("1")
	out, err := cmd.Call(ctx)
	require.NoError(t, err)
	want := []*listOutput{{
		AccountID: "000000000001",
		Name:      "test1",
	}}
	assert.Equal(t, want, out)
}
