package cmd

func init() {
	register(&Authz{command: command{
		name:    []string{"authz"},
		summary: "Authorize user access",
		usage:   "[options] user account-spec",
		minArgs: 2,
		maxArgs: 2,
	}})
}

type Authz struct{ command }

func (cmd *Authz) Run(ctx *Ctx, args []string) error {
	c := ctx.AWS()
	user := args[0]
	match, err := getAccounts(c, args[1])
	if err != nil {
		return err
	}
	out := make([]*AccountResultOutput, 0, len(match))
	for _, ac := range match {
		out = append(out, newAccountResult(ac, c.CreateAdminRole(ac.ID, user)))
	}
	return cmd.PrintOutput(out)
}
