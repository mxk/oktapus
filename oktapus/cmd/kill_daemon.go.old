package cmd

import (
	"github.com/LuminalHQ/cloudcover/oktapus/daemon"
	"github.com/LuminalHQ/cloudcover/oktapus/op"
	"github.com/LuminalHQ/cloudcover/x/cli"
)

var killDaemonCli = register(&cli.Info{
	Name:    "kill-daemon",
	Usage:   "[options]",
	Summary: "Terminate daemon process",
	New:     func() cli.Cmd { return &killDaemon{} },
})

type killDaemon struct {
	All    bool `flag:"Kill all daemons"`
	Others bool `flag:"Kill non-active daemons"`
}

func (cmd *killDaemon) Info() *cli.Info { return killDaemonCli }

func (cmd *killDaemon) Help(w *cli.Writer) {
	w.Text(`
	A daemon process maintains persistent account credential and information
	cache. A separate daemon is started for each unique authentication context,
	which is derived from environment variables such as OKTA_ORG,
	AWS_ACCESS_KEY_ID, etc.

	Normally, there is no need to kill daemons. They terminate automatically
	when no longer needed. This command forces a daemon to terminate.

	The 'active' daemon is one that would be used for executing commands in the
	current authentication context. If no options are specified, only the active
	daemon is killed.
	`)
	accountSpecHelp(w)
}

func (cmd *killDaemon) Main(args []string) error {
	return cmd.Run(op.NewCtx(), args)
}

func (cmd *killDaemon) Run(ctx *op.Ctx, args []string) error {
	active := cmd.All || !cmd.Others
	others := cmd.All || cmd.Others
	daemon.Kill(ctx, active, others)
	return nil
}
