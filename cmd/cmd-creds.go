package cmd

import (
	"github.com/LuminalHQ/oktapus/internal"
	"github.com/aws/aws-sdk-go/aws/credentials"
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
	SessionToken    string `printer:",width=1,last"`
	Error           string
}

func (cmd *Creds) Run(ctx *Ctx, args []string) error {
	c := ctx.AWS()
	match, err := getAccounts(c, args[0])
	if err != nil {
		return err
	}
	out := make([]*CredsOutput, 0, len(match))
	for _, ac := range match {
		out = append(out, newCredsOutput(ac, c.Creds(ac.ID)))
	}
	return cmd.PrintOutput(out)
}

func newCredsOutput(ac *Account, cr *credentials.Credentials) *CredsOutput {
	v, err := cr.Get()
	return &CredsOutput{
		AccountID:       ac.ID,
		Name:            ac.Name,
		AccessKeyID:     v.AccessKeyID,
		SecretAccessKey: v.SecretAccessKey,
		SessionToken:    v.SessionToken,
		Error:           explainError(err),
	}
}

func (o *CredsOutput) PrintRow(p *internal.Printer) {
	if o.Error == "" {
		internal.PrintRow(p, o)
	} else {
		p.PrintCol(0, o.AccountID, true)
		p.PrintCol(1, o.Name, true)
		p.PrintErr(o.Error)
	}
}
