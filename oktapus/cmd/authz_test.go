package cmd

import (
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/iam"
	"github.com/mxk/oktapus/mock"
	"github.com/mxk/oktapus/op"
	"github.com/stretchr/testify/require"
)

func TestAuthz(t *testing.T) {
	ctx, w := mockOrg(mock.Ctx, "test1", "test2")
	cmd := authzCli.New().(*authzCmd)
	cmd.Spec = "test1"
	cmd.Principals = []string{"user/user1"}

	out, err := cmd.Run(ctx)
	require.NoError(t, err)
	want := []*roleOutput{{
		Account: "000000000001",
		Name:    "test1",
		Role:    "arn:aws:iam::000000000001:role/oktapus/user1",
		Result:  "CREATED",
	}}
	require.Equal(t, want, out)

	w.Account("3").DataTypeRouter().Set(&iam.GetRoleOutput{},
		op.Error("access denied"))

	cmd.Spec = "test1,test2"
	cmd.Principals = []string{"user/user1", "user/user2"}
	out, err = cmd.Run(ctx)
	require.NoError(t, err)
	want = []*roleOutput{{
		Account: "000000000001",
		Name:    "test1",
		Role:    "arn:aws:iam::000000000001:role/oktapus/user1",
		Result:  "UPDATED",
	}, {
		Account: "000000000001",
		Name:    "test1",
		Role:    "arn:aws:iam::000000000001:role/oktapus/user2",
		Result:  "CREATED",
	}, {
		Account: "000000000002",
		Name:    "test2",
		Role:    "arn:aws:iam::000000000002:role/oktapus/user1",
		Result:  "CREATED",
	}, {
		Account: "000000000002",
		Name:    "test2",
		Role:    "arn:aws:iam::000000000002:role/oktapus/user2",
		Result:  "CREATED",
	}}
	require.Equal(t, want, out)
}
