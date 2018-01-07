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
	c := ctx.AWS()
	match, err := getAccounts(c, args[0])
	if err != nil {
		return err
	}
	out := make([]*AccountResultOutput, 0, len(match))
	for _, ac := range match {
		out = append(out, newAccountResult(ac, ac.Init()))
	}
	return cmd.PrintOutput(out, nil, nil)
}

func newAccountResult(ac *Account, err error) *AccountResultOutput {
	result := "OK"
	if err != nil {
		result = "ERROR: " + explainError(err)
	}
	return &AccountResultOutput{
		AccountID: ac.ID,
		Name:      ac.Name,
		Result:    result,
	}
}
