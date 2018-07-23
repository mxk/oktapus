package cmd

import (
	"bufio"
	"flag"

	"github.com/LuminalHQ/cloudcover/oktapus/op"
)

func init() {
	op.Register(&op.CmdInfo{
		Names:   []string{"update", "tag"},
		Summary: "Update account tags and/or description",
		Usage:   "[options] account-spec [tags]",
		MinArgs: 1,
		MaxArgs: 2,
		New:     func() op.Cmd { return &update{Name: "update"} },
	})
}

type update struct {
	Name
	PrintFmt
	Desc *string
	Spec string
	Set  op.Tags
	Clr  op.Tags
}

func (cmd *update) Help(w *bufio.Writer) {
	op.WriteHelp(w, `
		Update account tags and/or description.

		To set or clear tags, specify them as a comma-separated list after the
		account-spec. Use the '!' prefix to clear existing tags. You may need to
		escape the '!' character with a backslash, or quote the entire argument,
		to inhibit shell expansion.
	`)
	accountSpecHelp(w)
}

func (cmd *update) FlagCfg(fs *flag.FlagSet) {
	cmd.PrintFmt.FlagCfg(fs)
	op.StringPtrVar(fs, &cmd.Desc, "desc", "Set account `description`")
}

func (cmd *update) Run(ctx *op.Ctx, args []string) error {
	padArgs(cmd, &args)
	set, clr, err := op.ParseTags(args[1])
	if err != nil {
		return err
	}
	if cmd.Desc == nil && len(set)+len(clr) == 0 {
		op.UsageErrf(cmd, "either description or tags must be specified")
	}
	cmd.Spec, cmd.Set, cmd.Clr = args[0], set, clr
	out, err := ctx.Call(cmd)
	if err == nil {
		err = cmd.Print(out)
	}
	return err
}

func (cmd *update) Call(ctx *op.Ctx) (interface{}, error) {
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
