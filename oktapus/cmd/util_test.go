package cmd

import (
	"github.com/LuminalHQ/cloudcover/oktapus/mock"
	"github.com/LuminalHQ/cloudcover/oktapus/op"
)

// newCtx returns a Ctx for testing commands, optionally initializing account
// control information for the specified account IDs.
func newCtx(init ...string) *op.Ctx {
	ctx := &op.Ctx{Sess: mock.NewSession()}
	if len(init) > 0 {
		if err := initCtl(ctx, nil, init...); err != nil {
			panic(err)
		}
	}
	return ctx
}

// initCtl initializes account control information for unit tests.
func initCtl(ctx *op.Ctx, ctl *op.Ctl, ids ...string) error {
	var empty op.Ctl
	if ctl == nil {
		ctl = &empty
	}
	gw := ctx.Gateway()
	for _, id := range ids {
		id = mock.AccountID(id)
		ac := op.NewAccount(id, "")
		ac.Init(gw, gw.CredsProvider(id))
		if err := ctl.Init(ac.IAM()); err != nil {
			return err
		}
	}
	return nil
}
