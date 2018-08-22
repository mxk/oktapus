package cmd

import (
	"testing"

	"github.com/LuminalHQ/cloudcover/oktapus/mock"
	"github.com/LuminalHQ/cloudcover/oktapus/op"
	"github.com/LuminalHQ/cloudcover/x/fast"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAlloc(t *testing.T) {
	fast.MockSleep(-1)
	defer fast.MockSleep(0)

	ctx, w := mockOrg(mock.Ctx, "test1", "test2")
	setCtl(w, op.Ctl{Tags: op.Tags{"test"}}, "1", "2")

	cmd := allocCmd{Spec: "test"}
	out, err := cmd.Run(ctx)
	require.NoError(t, err)
	want := []*resultsOutput{{
		Account: "000000000001",
		Name:    "test1",
		Result:  "OK",
	}, {
		Account: "000000000002",
		Name:    "test2",
		Result:  "OK",
	}}
	assert.Equal(t, want, out)

	cmd = allocCmd{Spec: "test"}
	out, err = cmd.Run(ctx)
	assert.NoError(t, err)
	assert.Empty(t, out)

	cmd = allocCmd{Num: 1, Spec: "test"}
	cmd.Spec = "test"
	out, err = cmd.Run(ctx)
	assert.EqualError(t, err, "not enough accounts, need 1 more")
	assert.Nil(t, out)
}
