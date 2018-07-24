package cmd

import (
	"github.com/LuminalHQ/cloudcover/oktapus/op"
	"github.com/LuminalHQ/cloudcover/x/cli"
)

var listCli = register(&cli.Info{
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
	return cmd.Run(op.NewCtx(), args)
}

func (cmd *listCmd) Run(ctx *op.Ctx, args []string) error {
	padArgs(cmd, &args)
	cmd.Spec = args[0]
	out, err := ctx.Call(cmd)
	if err == nil {
		err = cmd.Print(out)
	}
	return err
}

func (cmd *listCmd) Call(ctx *op.Ctx) (interface{}, error) {
	if cmd.Refresh {
		ctx.All = nil
	}
	acs, err := ctx.Accounts(cmd.Spec)
	return listAccounts(acs), err
}
