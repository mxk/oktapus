package cmd

import (
	"testing"

	"github.com/LuminalHQ/cloudcover/oktapus/mock"
	"github.com/LuminalHQ/cloudcover/oktapus/op"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestList(t *testing.T) {
	ctx, w := mockOrg(mock.Ctx, "test1", "test2")
	setCtl(w, op.Ctl{}, "1")

	cmd := listCmd{}
	out, err := cmd.Run(ctx)
	require.NoError(t, err)
	want := []*listOutput{{
		Account: "000000000001",
		Name:    "test1",
	}}
	assert.Equal(t, want, out)

	id := *w.Root().OrgRouter().NewAccount("new").Id
	w.Account(id).RoleRouter()

	cmd = listCmd{Refresh: true, Spec: "all"}
	out, err = cmd.Run(ctx)
	require.NoError(t, err)
	want = []*listOutput{{
		Account: "000000000000",
		Name:    "master",
		Error:   op.ErrNoCtl.Error(),
	}, {
		Account: "000000000003",
		Name:    "new",
		Error:   op.ErrNoCtl.Error(),
	}, {
		Account: "000000000001",
		Name:    "test1",
	}, {
		Account: "000000000002",
		Name:    "test2",
		Error:   op.ErrNoCtl.Error(),
	}}
	assert.Equal(t, want, out)
}
