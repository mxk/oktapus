package cmd

func init() {
	register(&Authz{command: command{
		name:    []string{"authz"},
		summary: "Authorize user access",
		usage:   "[options] account-spec user [user ...]",
		minArgs: 2,
	}})
}

type Authz struct{ command }

func (cmd *Authz) Run(ctx *Ctx, args []string) error {
	match, err := ctx.Accounts(args[0])
	if err != nil {
		return err
	}
	// TODO: Use match.Apply
	c := ctx.AWS()
	for _, ac := range match {
		if ac.Err != nil {
			continue
		}
		for _, user := range args[1:] {
			if ac.Err = c.CreateAdminRole(ac.ID, user); ac.Err != nil {
				break
			}
		}
	}
	return cmd.PrintOutput(listResults(match))
}
