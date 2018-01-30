package cmd

import "flag"

func init() {
	register(&cmdInfo{
		names:   []string{"list", "ls"},
		summary: "List accounts",
		usage:   "[options] [account-spec]",
		maxArgs: 1,
		new:     func() Cmd { return &List{Name: "list"} },
	})
}

type List struct {
	Name
	PrintFmt
	refresh bool
}

func (cmd *List) FlagCfg(fs *flag.FlagSet) {
	cmd.PrintFmt.FlagCfg(fs)
	fs.BoolVar(&cmd.refresh, "refresh", false, "Refresh account information")
}

func (cmd *List) Run(ctx *Ctx, args []string) error {
	if cmd.refresh {
		if err := ctx.AWS().Refresh(); err != nil {
			return err
		}
	}
	padArgs(cmd, &args)
	match, err := ctx.Accounts(args[0])
	if err != nil {
		return err
	}
	return cmd.Print(listAccounts(match))
}
