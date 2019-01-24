package cmd

import (
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/mxk/oktapus/mock"
	"github.com/mxk/oktapus/op"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTag(t *testing.T) {
	ctx, w := mockOrg(mock.Ctx, "test1", "test2", "test3")
	setCtl(w, op.Ctl{}, "3")

	cmd := tagCmd{Init: true, Spec: "test1,test2,test3", Set: op.Tags{"init"}}
	out, err := cmd.Run(ctx)
	require.NoError(t, err)
	want := []*listOutput{{
		Account: "000000000001",
		Name:    "test1",
		Tags:    "init",
	}, {
		Account: "000000000002",
		Name:    "test2",
		Tags:    "init",
	}, {
		Account: "000000000003",
		Name:    "test3",
		Error:   "already initialized",
	}}
	assert.Equal(t, want, out)

	cmd = tagCmd{
		Desc: aws.String("desc"),
		Spec: "init",
		Set:  op.Tags{"set"},
		Clr:  op.Tags{"init"},
	}
	out, err = cmd.Run(ctx)
	require.NoError(t, err)
	want = []*listOutput{{
		Account:     "000000000001",
		Name:        "test1",
		Description: "desc",
		Tags:        "set",
	}, {
		Account:     "000000000002",
		Name:        "test2",
		Description: "desc",
		Tags:        "set",
	}}
	assert.Equal(t, want, out)
}
