package cmd

import "flag"

func init() {
	register(&List{command: command{
		name:    []string{"list", "ls"},
		summary: "List accounts",
		usage:   "[options] [account-spec]",
		maxArgs: 1,
	}})
}

type List struct {
	command
	refresh bool
}

func (cmd *List) FlagCfg(fs *flag.FlagSet) {
	cmd.command.FlagCfg(fs)
	fs.BoolVar(&cmd.refresh, "refresh", false, "Refresh account information")
}

func (cmd *List) Run(ctx *Ctx, args []string) error {
	if cmd.refresh {
		if err := ctx.AWS().Refresh(); err != nil {
			return err
		}
	}
	cmd.PadArgs(&args)
	match, err := ctx.Accounts(args[0])
	if err != nil {
		return err
	}
	return cmd.PrintOutput(listAccounts(match))
}
