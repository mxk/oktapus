package cmd

import (
	"github.com/mxk/go-cli"
	"github.com/mxk/oktapus/daemon"
)

var killDaemonCli = cli.Main.Add(&cli.Info{
	Name:    "kill-daemon",
	Usage:   "[addr]",
	Summary: "Terminate daemon process",
	MaxArgs: 1,
	New:     func() cli.Cmd { return killDaemonCmd{} },
})

type killDaemonCmd struct{}

func (killDaemonCmd) Info() *cli.Info { return killDaemonCli }

func (killDaemonCmd) Help(w *cli.Writer) {
	w.Text(`
	The daemon maintains account credentials and control information. It is
	normally started by the first oktapus command and continues running in the
	background, listening on ` + string(daemon.DefaultAddr) + ` by default.

	Killing the daemon forces the client to refresh the list of accounts and all
	credentials, which can be useful for debugging purposes.
	`)
}

func (killDaemonCmd) Main(args []string) error {
	return daemonAddr(args).Kill()
}
