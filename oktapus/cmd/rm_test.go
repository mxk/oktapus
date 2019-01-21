package cmd

import (
	"errors"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/mxk/cloudcover/oktapus/mock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRm(t *testing.T) {
	ctx, w := mockOrg(mock.Ctx, "test1", "test2", "test3")

	rr1 := w.Account("1").RoleRouter()
	rr1["r1"] = &mock.Role{}

	ur1 := w.Account("1").UserRouter()
	ur1["u1"] = &mock.User{}

	ur2 := w.Account("2").UserRouter()
	ur2["u1"] = &mock.User{}
	ur2["u2"] = &mock.User{}

	require.NoError(t, ctx.CredsProvider(mock.AccountID("1")).Ensure(10*time.Minute))
	require.NoError(t, ctx.CredsProvider(mock.AccountID("2")).Ensure(10*time.Minute))
	w.Account("0").DataTypeRouter().Set(&sts.AssumeRoleOutput{}, errors.New("access denied"))

	cmd := rmCmd{Spec: "test1,test2,test3", Type: "role", Names: []string{"r1"}}
	out, err := cmd.Run(ctx)
	require.NoError(t, err)
	assert.Equal(t, []*rmOutput{{
		Account:  "000000000001",
		Name:     "test1",
		Resource: "r1",
		Result:   "OK",
	}, {
		Account:  "000000000002",
		Name:     "test2",
		Resource: "r1",
		Result:   "NOT FOUND",
	}, {
		Account: "000000000003",
		Name:    "test3",
		Result:  "ERROR: access denied",
	}}, out)
	assert.NotContains(t, rr1, "r1")

	cmd = rmCmd{Spec: "test1,test2,test3", Type: "user", Names: []string{"u1", "u2"}}
	out, err = cmd.Run(ctx)
	require.NoError(t, err)
	assert.Equal(t, []*rmOutput{{
		Account:  "000000000001",
		Name:     "test1",
		Resource: "u1",
		Result:   "OK",
	}, {
		Account:  "000000000001",
		Name:     "test1",
		Resource: "u2",
		Result:   "NOT FOUND",
	}, {
		Account:  "000000000002",
		Name:     "test2",
		Resource: "u1",
		Result:   "OK",
	}, {
		Account:  "000000000002",
		Name:     "test2",
		Resource: "u2",
		Result:   "OK",
	}, {
		Account: "000000000003",
		Name:    "test3",
		Result:  "ERROR: access denied",
	}}, out)
	assert.NotContains(t, ur1, "u1")
	assert.NotContains(t, ur2, "u1")
	assert.NotContains(t, ur2, "u2")
}
