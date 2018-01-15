package cmd

import "flag"

func init() {
	register(&Free{command: command{
		name:    []string{"free"},
		summary: "Release accounts",
		usage:   "[options] [account-spec]",
		minArgs: 0,
		maxArgs: 1,
	}})
}

type Free struct {
	command
	force bool
}

func (cmd *Free) FlagCfg(fs *flag.FlagSet) {
	cmd.command.FlagCfg(fs)
	fs.BoolVar(&cmd.force, "force", false,
		"Release account even if you are not the owner")
}

func (cmd *Free) Run(ctx *Ctx, args []string) error {
	cmd.PadArgs(&args)
	match, err := ctx.Accounts(args[0])
	if err != nil {
		return err
	}
	mod := match[:0]
	commonRole := ctx.AWS().CommonRole
	for _, ac := range match {
		if ac.Err == nil && (cmd.force || ac.Owner == commonRole) {
			ac.Owner = ""
			mod = append(mod, ac)
		}
	}
	return cmd.PrintOutput(listResults(mod.Save()))
}
