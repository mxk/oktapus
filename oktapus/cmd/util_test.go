package cmd

import (
	"github.com/LuminalHQ/cloudcover/oktapus/mock"
	"github.com/LuminalHQ/cloudcover/oktapus/op"
	"github.com/LuminalHQ/cloudcover/x/arn"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/iam"
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
