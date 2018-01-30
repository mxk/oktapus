package cmd

import "flag"

func init() {
	register(&cmdInfo{
		names:   []string{"test"},
		summary: "command for testing... stuff",
		usage:   "...",
		maxArgs: -1,
		hidden:  true,
		new:     func() Cmd { return &Test{Name: "test"} },
	})
}

type Test struct{ Name }

func (Test) FlagCfg(fs *flag.FlagSet) {}

func (Test) Run(ctx *Ctx, args []string) error {
	ctx.Okta()
	return nil
}
