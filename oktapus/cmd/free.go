package cmd

import (
	"bufio"
	"flag"

	"github.com/LuminalHQ/cloudcover/oktapus/awsx"
	"github.com/LuminalHQ/cloudcover/oktapus/op"
)

func init() {
	op.Register(&op.CmdInfo{
		Names:   []string{"free"},
		Summary: "Release accounts",
		Usage:   "[options] [account-spec]",
		MinArgs: 0,
		MaxArgs: 1,
		New:     func() op.Cmd { return &free{Name: "free"} },
	})
}

type free struct {
	Name
	PrintFmt
	Force bool
	Spec  string
}

func (cmd *free) Help(w *bufio.Writer) {
	op.WriteHelp(w, `
		Release owned accounts.

		Freeing an account allows someone else to allocate it. If the account
		contains any temporary IAM users or roles, those are deleted (see authz
		and creds commands for more info).
	`)
	op.AccountSpecHelp(w)
}

func (cmd *free) FlagCfg(fs *flag.FlagSet) {
	cmd.PrintFmt.FlagCfg(fs)
	fs.BoolVar(&cmd.Force, "force", false,
		"Release account even if you are not the owner")
}

func (cmd *free) Run(ctx *op.Ctx, args []string) error {
	padArgs(cmd, &args)
	cmd.Spec = args[0]
	out, err := ctx.Call(cmd)
	if err == nil {
		err = cmd.Print(out)
	}
	return err
}

func (cmd *free) Call(ctx *op.Ctx) (interface{}, error) {
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
		go func() { ch <- awsx.DeleteRoles(ac.IAM(), op.TmpIAMPath) }()
		ac.Err = awsx.DeleteUsers(ac.IAM(), op.TmpIAMPath)
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
