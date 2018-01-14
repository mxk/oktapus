package cmd

import (
	"time"

	"github.com/LuminalHQ/oktapus/internal"
)

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
	//ctx.Okta()
	for i := 0; i < 10; i++ {
		log.I("%v", internal.Time())
		time.Sleep(time.Second)
	}
	return nil
}
