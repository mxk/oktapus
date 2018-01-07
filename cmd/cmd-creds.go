package cmd

import (
	"oktapus/awsgw"
	"oktapus/internal"
)

func init() {
	register(&Creds{command: command{
		name:    []string{"creds"},
		summary: "Get account credentials",
		usage:   "[options] account-spec",
		minArgs: 1,
		maxArgs: 1,
	}})
}

type Creds struct{ command }

type CredsOutput struct {
	AccountID       string
	Name            string
	Expires         string // TODO: Implement
	AccessKeyID     string
	SecretAccessKey string
	SessionToken    string `printer:"last"`
	Error           string
}

func (cmd *Creds) Run(ctx *Ctx, args []string) error {
	c := ctx.AWS()
	match, err := getAccounts(c, args[0])
	if err != nil {
		err = cmd.PrintOutput(cmd.out(cmd.Get(c, match)))
	}
	return err
}

func (cmd *Creds) Get(c *awsgw.Client, match []*Account) []*CredsOutput {
	out := make([]*CredsOutput, 0, len(match))
	for _, ac := range match {
		v, err := c.Creds(ac.ID).Get()
		out = append(out, &CredsOutput{
			AccountID:       ac.ID,
			Name:            ac.Name,
			AccessKeyID:     v.AccessKeyID,
			SecretAccessKey: v.SecretAccessKey,
			SessionToken:    v.SessionToken,
			Error:           explainError(err),
		})
	}
	return out
}

func (cmd *Creds) out(out []*CredsOutput) ([]*CredsOutput, internal.PrintCfgFunc, internal.PrintFunc) {
	return out, cmd.printCfg, cmd.print
}

func (*Creds) printCfg(p *internal.Printer) {
	// SessionTokens are too long to display in a table output
	if i := p.ColIdx("SessionToken"); p.Widths[i] > 20 {
		p.Widths[i] = 20
	}
}

func (*Creds) print(p *internal.Printer, v interface{}) {
	ac := v.(*CredsOutput)
	if ac.Error == "" {
		internal.DefaultPrintFunc(p, v)
		return
	}
	p.PrintCol(0, ac.AccountID, true)
	p.PrintCol(1, ac.Name, true)
	p.PrintErr(ac.Error)
}
