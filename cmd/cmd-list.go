package cmd

// TODO: Document 'all' and 'mine' implicit tags. Create common account-spec
// help.

func init() {
	register(&List{command: command{
		name:    []string{"list", "ls"},
		summary: "List accounts",
		usage:   "[options] account-spec",
		maxArgs: 1,
		help:    "List accounts in the organization.", // TODO: Improve
	}})
}

type List struct{ command }

func (cmd *List) Run(ctx *Ctx, args []string) error {
	cmd.PadArgs(&args)
	match, err := ctx.Accounts(args[0])
	if err != nil {
		return err
	}
	return cmd.PrintOutput(listAccounts(match))
}
