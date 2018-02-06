package cmd

import "github.com/LuminalHQ/oktapus/op"

func init() {
	op.Register(&op.CmdInfo{
		Names:   []string{"test"},
		Summary: "command for testing... stuff",
		Usage:   "...",
		MaxArgs: -1,
		Hidden:  true,
		New:     func() op.Cmd { return &test{Name: "test"} },
	})
}

type test struct {
	Name
	noFlags
}

func (test) Run(ctx *op.Ctx, args []string) error {
	ctx.Okta()
	return nil
}
