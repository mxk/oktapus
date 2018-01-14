package cmd

func init() {
	register(&Init{command: command{
		name:    []string{"init"},
		summary: "Initialize account control information",
		usage:   "[options] account-spec",
		minArgs: 1,
		maxArgs: 1,
	}})
}

type Init struct{ command }

type AccountResultOutput struct {
	AccountID string
	Name      string
	Result    string
}

func (cmd *Init) Run(ctx *Ctx, args []string) error {
	match, err := ctx.Accounts(args[0])
	if err != nil {
		return err
	}
	for _, ac := range match {
		ac.Ctl = new(Ctl)
	}
	return cmd.PrintOutput(listResults(match.Save(true)))
}

func listResults(acs Accounts) []*AccountResultOutput {
	out := make([]*AccountResultOutput, 0, len(acs))
	for _, ac := range acs {
		result := "OK"
		if ac.Err != nil {
			result = "ERROR: " + explainError(ac.Err)
		}
		out = append(out, &AccountResultOutput{
			AccountID: ac.ID,
			Name:      ac.Name,
			Result:    result,
		})
	}
	return out
}
