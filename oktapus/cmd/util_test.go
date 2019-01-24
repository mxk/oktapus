package cmd

import (
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	"github.com/mxk/go-cloud/aws/arn"
	"github.com/mxk/oktapus/mock"
	"github.com/mxk/oktapus/op"
	"github.com/stretchr/testify/assert"
)

func mockOrg(ctx arn.Ctx, accounts ...string) (*op.Ctx, *mock.AWS) {
	c := op.NewCtx()
	w := mock.NewAWS(ctx, mock.NewOrg(ctx, "master", accounts...))
	for id := range w.Root().OrgRouter().Accounts {
		*w.Account(id) = mock.ChainRouter{mock.UserRouter{}, mock.RoleRouter{}}
	}
	if err := c.Init(&w.Cfg); err != nil {
		panic(err)
	}
	return c, w
}

func setCtl(w *mock.AWS, ctl op.Ctl, ids ...string) {
	s, err := ctl.Encode()
	if err != nil {
		panic(err)
	}
	role := w.Ctx.New("iam", "role", op.IAMPath, op.CtlRole)
	for _, id := range ids {
		id := mock.AccountID(id)
		w.Account(id).RoleRouter()[op.CtlRole] = &mock.Role{Role: iam.Role{
			Arn:         arn.String(role.WithAccount(id)),
			Description: aws.String(s),
			Path:        aws.String(op.IAMPath),
			RoleName:    aws.String(op.CtlRole),
		}}
	}
}

func TestSplitPathName(t *testing.T) {
	tests := []struct {
		in         string
		tmp        bool
		path, name string
	}{
		{"", false, op.IAMPath, ""},
		{"", true, op.IAMTmpPath, ""},
		{"x", false, op.IAMPath, "x"},
		{"x", true, op.IAMTmpPath, "x"},
		{"/x", false, "/", "x"},
		{"/x", true, op.IAMTmpPath, "x"},
		{"/x/y", false, "/x/", "y"},
		{"/x/y", true, op.IAMTmpPath + "x/", "y"},
	}
	for _, tc := range tests {
		path, name, err := splitPathName(tc.in, tc.tmp)
		assert.NoError(t, err, "%+v", tc)
		assert.Equal(t, tc.path, path, "%+v", tc)
		assert.Equal(t, tc.name, name, "%+v", tc)
	}
	_, _, err := splitPathName(":", false)
	assert.Error(t, err)
}
