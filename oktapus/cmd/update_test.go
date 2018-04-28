package cmd

import (
	"testing"

	"github.com/LuminalHQ/oktapus/op"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUpdate(t *testing.T) {
	cmd := newCmd("update", "-desc=desc").(*update)
	ctx := newCtx("1")

	cmd.Set = op.Tags{"set"}
	cmd.Spec = "test1"
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
