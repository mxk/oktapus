package cmd

import (
	"flag"

	"github.com/LuminalHQ/oktapus/daemon"
)

func init() {
	register(&cmdInfo{
		names:   []string{"kill-daemon"},
		summary: "Terminate daemon process",
		usage:   "[options]",
		new:     func() Cmd { return &killDaemon{Name: "kill-daemon"} },
	})
}

type killDaemon struct {
	Name
	active *bool
	others bool
}

func (cmd *killDaemon) FlagCfg(fs *flag.FlagSet) {
	BoolPtrVar(fs, &cmd.active, "active", "Kill the active daemon")
	fs.BoolVar(&cmd.others, "others", false, "Kill non-active daemons")
}

func (cmd *killDaemon) Run(ctx *Ctx, args []string) error {
	active := !cmd.others || (cmd.active != nil && *cmd.active)
	daemon.Kill(ctx, active, cmd.others)
	return nil
}
