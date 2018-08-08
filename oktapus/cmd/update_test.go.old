package cmd

import (
	"testing"

	"github.com/LuminalHQ/cloudcover/oktapus/op"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUpdate(t *testing.T) {
	desc := "desc"
	cmd := updateCmd{Desc: &desc, Set: op.Tags{"set"}, Spec: "test1"}
	ctx, _ := newCtx("1")

	out, err := cmd.Call(ctx)
	require.NoError(t, err)
	want := []*listOutput{{
		AccountID:   "000000000001",
		Name:        "test1",
		Description: "desc",
		Tags:        "set",
	}}
	assert.Equal(t, want, out)
}
