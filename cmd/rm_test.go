package cmd

import (
	"errors"
	"testing"

	"github.com/LuminalHQ/oktapus/mock"
	"github.com/aws/aws-sdk-go/service/sts"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRmFilter(t *testing.T) {
	ctx := newCtx()
	cmd := newCmd("rm").(*rm)

	dt := mock.NewDataTypeRouter()
	dt.Set(new(sts.AssumeRoleOutput), errors.New("access denied"))
	ctx.Sess.(*mock.Session).OrgsRouter().Account("").Add(dt)

	cmd.Type = "role"
	cmd.Spec = "test2"
	cmd.Names = []string{"a", "b"}
	out, err := cmd.Call(ctx)
	require.NoError(t, err)
	assert.Equal(t, []*rmOutput{{
		AccountID: "000000000002",
		Name:      "test2",
		Result:    "ERROR: access denied",
	}}, out)
}

func TestRmRole(t *testing.T) {
	ctx := newCtx()
	cmd := newCmd("rm").(*rm)

	r := ctx.Sess.(*mock.Session).OrgsRouter().Account("1").RoleRouter()
	r["a"] = &mock.Role{}
	r["b"] = &mock.Role{}

	cmd.Type = "role"
	cmd.Spec = "test1"
	cmd.Names = []string{"a", "b", "c"}
	out, err := cmd.Call(ctx)
	require.NoError(t, err)
	assert.Empty(t, r)
	assert.Equal(t, []*rmOutput{{
		AccountID: "000000000001",
		Name:      "test1",
		Resource:  "a",
		Result:    "OK",
	}, {
		AccountID: "000000000001",
		Name:      "test1",
		Resource:  "b",
		Result:    "OK",
	}, {
		AccountID: "000000000001",
		Name:      "test1",
		Resource:  "c",
		Result:    "ERROR: NoSuchEntity: Unknown role: c",
	}}, out)
}

func TestRmUser(t *testing.T) {
	ctx := newCtx()
	cmd := newCmd("rm").(*rm)

	r := ctx.Sess.(*mock.Session).OrgsRouter().Account("1").UserRouter()
	r["a"] = &mock.User{}

	cmd.Type = "user"
	cmd.Spec = "test1"
	cmd.Names = []string{"a"}
	out, err := cmd.Call(ctx)
	require.NoError(t, err)
	assert.Empty(t, r)
	assert.Equal(t, []*rmOutput{{
		AccountID: "000000000001",
		Name:      "test1",
		Resource:  "a",
		Result:    "OK",
	}}, out)
}
