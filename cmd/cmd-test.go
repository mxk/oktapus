package cmd

import "flag"

func init() {
	register(&cmdInfo{
		names:   []string{"test"},
		summary: "command for testing... stuff",
		usage:   "...",
		maxArgs: -1,
		hidden:  true,
		new:     func() Cmd { return &test{Name: "test"} },
	})
}

type test struct{ Name }

func (test) FlagCfg(fs *flag.FlagSet) {}

func (test) Run(ctx *Ctx, args []string) error {
	ctx.Okta()
	return nil
}
