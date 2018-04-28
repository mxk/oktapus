package cmd

import (
	"flag"

	"github.com/LuminalHQ/cloudcover/oktapus/op"
)

func init() {
	op.Register(&op.CmdInfo{
		Names:   []string{"list", "ls"},
		Summary: "List accounts",
		Usage:   "[options] [account-spec]",
		MaxArgs: 1,
		New:     func() op.Cmd { return &list{Name: "list"} },
	})
}

type list struct {
	Name
	PrintFmt
	Refresh bool
	Spec    string
}

func (cmd *list) FlagCfg(fs *flag.FlagSet) {
	cmd.PrintFmt.FlagCfg(fs)
	fs.BoolVar(&cmd.Refresh, "refresh", false, "Refresh account information")
}

func (cmd *list) Run(ctx *op.Ctx, args []string) error {
	padArgs(cmd, &args)
	cmd.Spec = args[0]
	out, err := ctx.Call(cmd)
	if err == nil {
		err = cmd.Print(out)
	}
	return err
}

func (cmd *list) Call(ctx *op.Ctx) (interface{}, error) {
	if cmd.Refresh {
		ctx.All = nil
	}
	acs, err := ctx.Accounts(cmd.Spec)
	return listAccounts(acs), err
}
