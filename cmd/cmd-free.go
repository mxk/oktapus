package cmd

import "flag"

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
		ac.Err = delTmpUsers(ac.IAM)
		if err := delTmpRoles(ac.IAM); ac.Err == nil {
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
