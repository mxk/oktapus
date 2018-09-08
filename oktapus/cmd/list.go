package cmd

import (
	"github.com/LuminalHQ/cloudcover/oktapus/op"
	"github.com/LuminalHQ/cloudcover/oktapus/table"
	"github.com/LuminalHQ/cloudcover/x/cli"
)

var listCli = cli.Main.Add(&cli.Info{
	Name:    "ls",
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

func (*listCmd) Info() *cli.Info { return listCli }

func (*listCmd) Help(w *cli.Writer) {
	w.Text(`
	List accounts.

	By default, this command lists only accessible and initialized accounts. Use
	"all" to list all known accounts. Use the 'tag' command to initialize
	account control (-init option) and set account tags.

	In rare circumstances, it may be helpful to run 'kill-daemon' command to
	reset cache when diagnosing access problems.
	`)
	accountSpecHelp(w)
}

func (cmd *listCmd) Main(args []string) error {
	cmd.Spec = get(args, 0)
	return op.RunAndPrint(cmd)
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
	Error       string `json:",omitempty"`
}

func listAccounts(acs op.Accounts) []*listOutput {
	out := make([]*listOutput, len(acs))
	for i, ac := range acs.CtlOrErr() {
		out[i] = &listOutput{
			Account:     ac.ID,
			Name:        ac.Name,
			Owner:       ac.Ctl.Owner,
			Description: ac.Ctl.Desc,
			Tags:        ac.Ctl.Tags.String(),
			Error:       explainError(ac.Err),
		}
	}
	return out
}

func (o *listOutput) PrintRow(p *table.Printer) {
	if o.Error == "" {
		table.PrintRow(p, o)
	} else {
		p.PrintCol(0, o.Account, true)
		p.PrintCol(1, o.Name, true)
		p.PrintErr(o.Error)
	}
}
