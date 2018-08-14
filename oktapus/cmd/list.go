package cmd

import (
	"github.com/LuminalHQ/cloudcover/oktapus/internal"
	"github.com/LuminalHQ/cloudcover/oktapus/op"
	"github.com/LuminalHQ/cloudcover/x/cli"
)

var listCli = cli.Main.Add(&cli.Info{
	Name:    "list|ls",
	Usage:   "[options] [account-spec]",
	Summary: "List accounts",
	MaxArgs: 1,
	New:     func() cli.Cmd { return &listCmd{} },
})

type listCmd struct {
	OutFmt
	Refresh bool `flag:"Refresh account information"`
	Spec    string
}

func (cmd *listCmd) Info() *cli.Info { return listCli }

func (cmd *listCmd) Help(w *cli.Writer) {
	w.Text("List accounts.")
	accountSpecHelp(w)
}

func (cmd *listCmd) Main(args []string) error {
	cmd.Spec = get(args, 0)
	out, err := op.Run(cmd)
	if err == nil {
		err = cmd.Print(out)
	}
	return err
}

func (cmd *listCmd) Run(ctx *op.Ctx) (interface{}, error) {
	if cmd.Refresh {
		if err := ctx.Refresh(); err != nil {
			return nil, err
		}
	}
	acs, err := ctx.Match(cmd.Spec)
	return listAccounts(acs), err
}

type listOutput struct {
	Account     string
	Name        string
	Owner       string
	Description string
	Tags        string `printer:",last"`
	Error       string
}

func listAccounts(acs op.Accounts) []*listOutput {
	out := make([]*listOutput, 0, len(acs))
	for _, ac := range acs {
		err := ac.Err
		if err == nil && !ac.CtlValid() {
			err = op.ErrNoCtl
		}
		out = append(out, &listOutput{
			Account:     ac.ID,
			Name:        ac.Name,
			Owner:       ac.Ctl.Owner,
			Description: ac.Ctl.Desc,
			Tags:        ac.Ctl.Tags.String(),
			Error:       explainError(err),
		})
	}
	return out
}

func (o *listOutput) PrintRow(p *internal.Printer) {
	if o.Error == "" {
		internal.PrintRow(p, o)
	} else {
		p.PrintCol(0, o.Account, true)
		p.PrintCol(1, o.Name, true)
		p.PrintErr(o.Error)
	}
}
