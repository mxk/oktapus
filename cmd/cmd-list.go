package cmd

import "flag"

func init() {
	register(&cmdInfo{
		names:   []string{"list", "ls"},
		summary: "List accounts",
		usage:   "[options] [account-spec]",
		maxArgs: 1,
		new:     func() Cmd { return &list{Name: "list"} },
	})
}

type list struct {
	Name
	PrintFmt
	Refresh bool
	Spec    string
}

func (cmd *list) FlagCfg(fs *flag.FlagSet) {
	cmd.PrintFmt.FlagCfg(fs)
	fs.BoolVar(&cmd.Refresh, "refresh", false, "Refresh account information")
}

func (cmd *list) Run(ctx *Ctx, args []string) error {
	padArgs(cmd, &args)
	cmd.Spec = args[0]
	out, err := ctx.Call(cmd)
	if err == nil {
		err = cmd.Print(out.([]*listOutput))
	}
	return err
}

func (cmd *list) Call(ctx *Ctx) (interface{}, error) {
	if cmd.Refresh {
		ctx.all = nil
	}
	acs, err := ctx.Accounts(cmd.Spec)
	return listAccounts(acs), err
}
