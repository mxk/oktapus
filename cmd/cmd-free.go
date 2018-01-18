package cmd

import (
	"bufio"
	"flag"
)

func init() {
	register(&Free{command: command{
		name:    []string{"free"},
		summary: "Release accounts",
		usage:   "[options] [account-spec]",
		minArgs: 0,
		maxArgs: 1,
	}})
}

type Free struct {
	command
	force bool
}

func (cmd *Free) Help(w *bufio.Writer) {
	writeHelp(w, `
		Release owned accounts.

		Freeing an account allows someone else to allocate it. If the account
		contains any temporary IAM users or roles, those are deleted (see authz
		and creds commands for more info).
	`)
	accountSpecHelp(w)
}

func (cmd *Free) FlagCfg(fs *flag.FlagSet) {
	cmd.command.FlagCfg(fs)
	fs.BoolVar(&cmd.force, "force", false,
		"Release account even if you are not the owner")
}

func (cmd *Free) Run(ctx *Ctx, args []string) error {
	cmd.PadArgs(&args)
	acs, err := ctx.Accounts(args[0])
	if err != nil {
		return err
	}
	commonRole := ctx.AWS().CommonRole
	acs = acs.Filter(func(ac *Account) bool {
		return ac.Err == nil && (cmd.force || ac.Owner == commonRole)
	})

	// Clear owner and delete temporary users/roles
	acs.Apply(func(ac *Account) {
		ac.Owner = ""
		ch := make(chan error, 1)
		go func() { ch <- delTmpRoles(ac.IAM) }()
		ac.Err = delTmpUsers(ac.IAM)
		if err := <-ch; ac.Err == nil {
			ac.Err = err
		}
	})

	// Save owner changes only if all temporary users/roles were deleted
	tmp := append(make(Accounts, 0, len(acs)), acs...)
	tmp.Filter(func(ac *Account) bool {
		return ac.Err == nil
	}).Save()
	return cmd.PrintOutput(listResults(acs))
}
