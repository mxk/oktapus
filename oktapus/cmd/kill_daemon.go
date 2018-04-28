package cmd

import (
	"bufio"
	"flag"

	"github.com/LuminalHQ/cloudcover/oktapus/daemon"
	"github.com/LuminalHQ/cloudcover/oktapus/op"
)

func init() {
	op.Register(&op.CmdInfo{
		Names:   []string{"kill-daemon"},
		Summary: "Terminate daemon process",
		Usage:   "[options]",
		New:     func() op.Cmd { return &killDaemon{Name: "kill-daemon"} },
	})
}

type killDaemon struct {
	Name
	All    bool
	Others bool
}

func (cmd *killDaemon) Help(w *bufio.Writer) {
	op.WriteHelp(w, `
		A daemon process maintains persistent account credential and information
		cache. A separate daemon is started for each unique authentication
		context, which is derived from environment variables such as OKTA_ORG,
		AWS_ACCESS_KEY_ID, etc.

		Normally, there is no need to kill daemons. They terminate automatically
		when no longer needed. This command forces a daemon to terminate.

		The 'active' daemon is one that would be used for executing commands in
		the current authentication context. If no options are specified, only
		the active daemon is killed.
	`)
	op.AccountSpecHelp(w)
}

func (cmd *killDaemon) FlagCfg(fs *flag.FlagSet) {
	fs.BoolVar(&cmd.All, "all", false, "Kill all daemons")
	fs.BoolVar(&cmd.Others, "others", false, "Kill non-active daemons")
}

func (cmd *killDaemon) Run(ctx *op.Ctx, args []string) error {
	active := cmd.All || !cmd.Others
	others := cmd.All || cmd.Others
	daemon.Kill(ctx, active, others)
	return nil
}
