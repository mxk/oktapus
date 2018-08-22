package cmd

import (
	"github.com/LuminalHQ/cloudcover/oktapus/op"
	"github.com/LuminalHQ/cloudcover/x/cli"
	"github.com/LuminalHQ/cloudcover/x/fast"
)

var freeCli = cli.Main.Add(&cli.Info{
	Name:    "free",
	Usage:   "[options] [account-spec]",
	Summary: "Release allocated accounts",
	MinArgs: 0,
	MaxArgs: 1,
	New:     func() cli.Cmd { return &freeCmd{} },
})

type freeCmd struct {
	OutFmt
	Force bool `flag:"Free accounts owned by others"`
	Spec  string
}

func (*freeCmd) Info() *cli.Info { return freeCli }

func (*freeCmd) Help(w *cli.Writer) {
	w.Text(`
	Release allocated accounts.

	Freeing an account allows someone else to allocate it. Any temporary IAM
	users or roles are deleted (see authz and creds commands for more detail).
	`)
	accountSpecHelp(w)
}

func (cmd *freeCmd) Main(args []string) error {
	cmd.Spec = get(args, 0)
	if cmd.Spec == "" && cmd.Force {
		return cli.Error("-force requires account-spec")
	}
	return op.RunAndPrint(cmd)
}

func (cmd *freeCmd) Run(ctx *op.Ctx) (interface{}, error) {
	acs, err := ctx.Match(cmd.Spec)
	if err != nil {
		return nil, err
	}
	me := ctx.Role().Name()
	acs = acs.Filter(func(ac *op.Account) bool {
		return ac.CtlValid() && ac.Ctl.Owner != "" &&
			(ac.Ctl.Owner == me || cmd.Force)
	})

	// Delete temporary users/roles
	acs.Map(func(_ int, ac *op.Account) error {
		return fast.Call(
			func() error { return ac.IAM.DeleteRoles(op.IAMTmpPath) },
			func() error { return ac.IAM.DeleteUsers(op.IAMTmpPath) },
		)
	})

	// Clear owner if all temporary users/roles were deleted
	acs.Filter(func(ac *op.Account) bool {
		if ac.Err != nil {
			return false
		}
		ac.Ctl.Owner = ""
		return true
	}).StoreCtl()
	return listOwners(acs), nil
}
