package cmd

import (
	"testing"

	"github.com/LuminalHQ/cloudcover/oktapus/mock"
	"github.com/LuminalHQ/cloudcover/oktapus/op"
	"github.com/stretchr/testify/require"
)

func TestAuthz(t *testing.T) {
	ctx, w := mockOrg(mock.Ctx, "test1", "test2", "test3")
	setCtl(w, op.Ctl{}, "1", "2") // TODO: Should not be required
	cmd := authzCli.New().(*authzCmd)
	cmd.Spec = "test1"
	cmd.Roles = []string{"user1"}

	out, err := cmd.Run(ctx)
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
	out, err = cmd.Run(ctx)
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
