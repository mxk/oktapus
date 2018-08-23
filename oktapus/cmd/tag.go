package cmd

import (
	"github.com/LuminalHQ/cloudcover/oktapus/op"
	"github.com/LuminalHQ/cloudcover/x/cli"
)

var tagCli = cli.Main.Add(&cli.Info{
	Name:    "tag|set",
	Usage:   "[options] account-spec [tags]",
	Summary: "Set account tags and/or description",
	MinArgs: 1,
	MaxArgs: 2,
	New:     func() cli.Cmd { return &tagCmd{} },
})

type tagCmd struct {
	OutFmt
	Desc *string `flag:"Set account description"`
	Init bool    `flag:"Initialize account control"`
	Spec string
	Set  op.Tags
	Clr  op.Tags
}

func (*tagCmd) Info() *cli.Info { return tagCli }

func (*tagCmd) Help(w *cli.Writer) {
	w.Text(`
	Set account tags and/or description.

	The account owner, description, and tags are stored within each account in
	the OktapusAccountControl IAM role description. Accounts that do not have
	this role are not managed by oktapus. Use -init to create this role and set
	the initial description and tags.

	To set or clear tags, specify them as a comma-separated list after the
	account spec. Use '!' prefix to clear a tag. Escape '!' with a backslash or
	use single quotes around the entire argument to inhibit shell expansion.
	`)
	accountSpecHelp(w)
}

func (cmd *tagCmd) Main(args []string) error {
	tags := get(args, 1)
	if tags == "" && cmd.Init {
		tags = "init"
	}
	set, clr, err := op.ParseTags(tags)
	if err != nil {
		return err
	}
	if cmd.Desc == nil && len(set)+len(clr) == 0 {
		return cli.Error("either description or tags must be specified")
	}
	cmd.Spec, cmd.Set, cmd.Clr = args[0], set, clr
	return op.RunAndPrint(cmd)
}

func (cmd *tagCmd) Run(ctx *op.Ctx) (interface{}, error) {
	acs, err := ctx.Match(cmd.Spec)
	if err != nil {
		return nil, err
	}
	update := func(ac *op.Account) bool {
		if cmd.Desc != nil {
			ac.Ctl.Desc = *cmd.Desc
		}
		ac.Ctl.Tags.Apply(cmd.Set, cmd.Clr)
		return true
	}
	if cmd.Init {
		acs.EnsureCreds(minDur).Filter(func(ac *op.Account) bool {
			if ac.CredsValid() {
				if !ac.CtlValid() {
					return update(ac)
				}
				ac.Err = op.Error("already initialized")
			}
			return false
		}).InitCtl()
	} else {
		acs.Filter(func(ac *op.Account) bool {
			return ac.CtlValid() && update(ac)
		}).StoreCtl()
	}
	return listAccounts(acs), nil
}
