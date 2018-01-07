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
	c := ctx.AWS()
	match, err := getAccounts(c, args[0])
	if err != nil {
		return err
	}
	out := make([]*AccountResultOutput, 0, len(match))
	for _, ac := range match {
		ctl := ac.Ctl()
		if ac.Error() == nil && (cmd.force || ctl.Owner == c.CommonRole) {
			ctl.Owner = ""
			out = append(out, newAccountResult(ac, ac.Save()))
		}
	}
	return cmd.PrintOutput(out, nil, nil)
}
