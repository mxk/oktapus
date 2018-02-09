package cmd

import (
	"testing"

	"github.com/LuminalHQ/oktapus/mock"
	"github.com/LuminalHQ/oktapus/op"
	"github.com/stretchr/testify/require"
)

func TestAuthz(t *testing.T) {
	ctx := op.Ctx{Sess: mock.NewSession()}
	initCtl(&ctx, nil, "1", "2")

	cmd := newCmd("authz").(*authz)
	cmd.Spec = "test1"
	cmd.Roles = []string{"user1"}
	out, err := cmd.Call(&ctx)
	require.NoError(t, err)
	want := []*roleOutput{{
		AccountID: "000000000001",
		Name:      "test1",
		Role:      "user1",
		Result:    "CREATED",
	}}
	require.Equal(t, want, out)

	cmd.Spec = "test1,test2,test3"
	cmd.Roles = []string{"user1", "user2"}
	out, err = cmd.Call(&ctx)
	require.NoError(t, err)
	want = []*roleOutput{{
		AccountID: "000000000001",
		Name:      "test1",
		Role:      "user1",
		Result:    "UPDATED",
	}, {
		AccountID: "000000000001",
		Name:      "test1",
		Role:      "user2",
		Result:    "CREATED",
	}, {
		AccountID: "000000000002",
		Name:      "test2",
		Role:      "user1",
		Result:    "CREATED",
	}, {
		AccountID: "000000000002",
		Name:      "test2",
		Role:      "user2",
		Result:    "CREATED",
	}, {
		AccountID: "000000000003",
		Name:      "test3",
		Role:      "",
		Result:    "ERROR: account control not initialized",
	}}
	require.Equal(t, want, out)
}