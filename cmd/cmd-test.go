package cmd

func init() {
	register(&Test{command: command{
		name:    []string{"test"},
		summary: "command for testing... stuff",
		usage:   "...",
		maxArgs: -1,
		hidden:  true,
	}})
}

type Test struct{ command }

func (Test) Run(ctx *Ctx, args []string) error {
	return nil
}
