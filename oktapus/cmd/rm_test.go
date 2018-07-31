package cmd

import (
	"errors"
	"testing"

	"github.com/LuminalHQ/cloudcover/oktapus/mock"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRmFilter(t *testing.T) {
	ctx, s := newCtx()
	cmd := rmCmd{Spec: "test1", Type: "role", Names: []string{"a"}}

	dt := mock.NewDataTypeRouter()
	dt.Set(new(sts.AssumeRoleOutput), errors.New("access denied"))
	s.OrgsRouter().Account("").Add(dt)

	out, err := cmd.Call(ctx)
	require.NoError(t, err)
	assert.Equal(t, []*rmOutput{{
		AccountID: "000000000001",
		Name:      "test1",
		Result:    "ERROR: access denied",
	}}, out)
}

func TestRmRole(t *testing.T) {
	ctx, s := newCtx()
	cmd := rmCmd{Spec: "test1", Type: "role", Names: []string{"a", "b", "c"}}

	r := s.OrgsRouter().Account("1").RoleRouter()
	r["a"] = &mock.Role{}
	r["b"] = &mock.Role{}

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
	ctx, s := newCtx()
	cmd := rmCmd{Spec: "test2", Type: "user", Names: []string{"a"}}

	r := s.OrgsRouter().Account("2").UserRouter()
	r["a"] = &mock.User{}

	out, err := cmd.Call(ctx)
	require.NoError(t, err)
	assert.Empty(t, r)
	assert.Equal(t, []*rmOutput{{
		AccountID: "000000000002",
		Name:      "test2",
		Resource:  "a",
		Result:    "OK",
	}}, out)
}
