package cmd

import (
	"testing"

	"github.com/LuminalHQ/cloudcover/oktapus/op"
	"github.com/stretchr/testify/require"
)

func TestAuthz(t *testing.T) {
	ctx := newCtx("1", "2")
	cmd := newCmd("authz").(*authz)
	cmd.Spec = "test1"
	cmd.Roles = []string{"user1"}
	out, err := cmd.Call(ctx)
	require.NoError(t, err)
	want := []*roleOutput{{
		AccountID: "000000000001",
		Name:      "test1",
		Path:      op.IAMPath,
		Role:      "user1",
		Result:    "CREATED",
	}}
	require.Equal(t, want, out)

	cmd.Spec = "test1,test2,test3"
	cmd.Roles = []string{"user1", "/user2"}
	out, err = cmd.Call(ctx)
	require.NoError(t, err)
	want = []*roleOutput{{
		AccountID: "000000000001",
		Name:      "test1",
		Path:      op.IAMPath,
		Role:      "user1",
		Result:    "UPDATED",
	}, {
		AccountID: "000000000001",
		Name:      "test1",
		Path:      "/",
		Role:      "user2",
		Result:    "CREATED",
	}, {
		AccountID: "000000000002",
		Name:      "test2",
		Path:      op.IAMPath,
		Role:      "user1",
		Result:    "CREATED",
	}, {
		AccountID: "000000000002",
		Name:      "test2",
		Path:      "/",
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
