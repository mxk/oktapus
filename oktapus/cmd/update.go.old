package cmd

import (
	"github.com/LuminalHQ/cloudcover/oktapus/op"
	"github.com/LuminalHQ/cloudcover/x/cli"
)

var updateCli = register(&cli.Info{
	Name:    "update|tag",
	Usage:   "[options] account-spec [tags]",
	Summary: "Update account tags and/or description",
	MinArgs: 1,
	MaxArgs: 2,
	New:     func() cli.Cmd { return &updateCmd{} },
})

type updateCmd struct {
	OutFmt
	Desc *string `flag:"Set account <description>"`
	Spec string
	Set  op.Tags
	Clr  op.Tags
}

func (cmd *updateCmd) Info() *cli.Info { return updateCli }

func (cmd *updateCmd) Help(w *cli.Writer) {
	w.Text(`
	Update account tags and/or description.

	To set or clear tags, specify them as a comma-separated list after the
	account-spec. Use the '!' prefix to clear existing tags. You may need to
	escape the '!' character with a backslash, or quote the entire argument, to
	inhibit shell expansion.
	`)
	accountSpecHelp(w)
}

func (cmd *updateCmd) Main(args []string) error {
	return cmd.Run(op.NewCtx(), args)
}

func (cmd *updateCmd) Run(ctx *op.Ctx, args []string) error {
	padArgs(cmd, &args)
	set, clr, err := op.ParseTags(args[1])
	if err != nil {
		return err
	}
	if cmd.Desc == nil && len(set)+len(clr) == 0 {
		return cli.Error("either description or tags must be specified")
	}
	cmd.Spec, cmd.Set, cmd.Clr = args[0], set, clr
	out, err := ctx.Call(cmd)
	if err == nil {
		err = cmd.Print(out)
	}
	return err
}

func (cmd *updateCmd) Call(ctx *op.Ctx) (interface{}, error) {
	acs, err := ctx.Accounts(cmd.Spec)
	if err != nil {
		return nil, err
	}
	mod := acs[:0]
	for _, ac := range acs {
		if ac.Err == nil {
			if cmd.Desc != nil {
				ac.Desc = *cmd.Desc
			}
			ac.Tags.Apply(cmd.Set, cmd.Clr)
			mod = append(mod, ac)
		}
	}
	return listAccounts(mod.Save()), nil
}
