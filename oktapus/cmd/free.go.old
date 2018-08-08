package cmd

import (
	"github.com/LuminalHQ/cloudcover/oktapus/op"
	"github.com/LuminalHQ/cloudcover/x/cli"
)

var freeCli = register(&cli.Info{
	Name:    "free",
	Usage:   "[options] [account-spec]",
	Summary: "Release accounts",
	MinArgs: 0,
	MaxArgs: 1,
	New:     func() cli.Cmd { return &freeCmd{} },
})

type freeCmd struct {
	OutFmt
	Force bool `flag:"Release account even if you are not the owner"`
	Spec  string
}

func (cmd *freeCmd) Info() *cli.Info { return freeCli }

func (cmd *freeCmd) Help(w *cli.Writer) {
	w.Text(`
	Release owned accounts.

	Freeing an account allows someone else to allocate it. If the account
	contains any temporary IAM users or roles, those are deleted (see authz and
	creds commands for more info).
	`)
	accountSpecHelp(w)
}

func (cmd *freeCmd) Main(args []string) error {
	return cmd.Run(op.NewCtx(), args)
}

func (cmd *freeCmd) Run(ctx *op.Ctx, args []string) error {
	padArgs(cmd, &args)
	cmd.Spec = args[0]
	out, err := ctx.Call(cmd)
	if err == nil {
		err = cmd.Print(out)
	}
	return err
}

func (cmd *freeCmd) Call(ctx *op.Ctx) (interface{}, error) {
	acs, err := ctx.Accounts(cmd.Spec)
	if err != nil {
		return nil, err
	}
	name := ctx.Gateway().CommonRole.Name()
	acs = acs.Filter(func(ac *op.Account) bool {
		return ac.Err == nil && (cmd.Force || ac.Owner == name)
	})

	// Clear owner and delete temporary users/roles
	acs.Apply(func(_ int, ac *op.Account) {
		ac.Owner = ""
		ch := make(chan error, 1)
		go func() { ch <- ac.IAM().DeleteRoles(op.TmpIAMPath) }()
		ac.Err = ac.IAM().DeleteUsers(op.TmpIAMPath)
		if err := <-ch; ac.Err == nil {
			ac.Err = err
		}
	})

	// Save owner changes only if all temporary users/roles were deleted
	tmp := append(make(op.Accounts, 0, len(acs)), acs...)
	tmp.Filter(func(ac *op.Account) bool {
		return ac.Err == nil
	}).Save()
	return listResults(acs), nil
}
