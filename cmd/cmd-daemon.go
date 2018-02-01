package cmd

import (
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/LuminalHQ/oktapus/daemon"
)

func init() {
	register(&cmdInfo{
		names:   []string{"daemon"},
		summary: "Persistent daemon process",
		usage:   "[options] addr",
		minArgs: 1,
		maxArgs: 1,
		hidden:  true,
		new:     func() Cmd { return daemonCmd{Name: "daemon"} },
	})
}

type daemonCmd struct{ Name }

func (daemonCmd) FlagCfg(fs *flag.FlagSet) {}

func (daemonCmd) Run(ctx *Ctx, args []string) error {
	addr := args[0]
	call := daemon.Listen(addr)
	if _, err := os.Stat(addr); err == nil {
		defer os.Remove(addr)
	}
	const daemonTimeout = 36 * time.Hour
	timeout := time.NewTimer(daemonTimeout)
	ticker := time.NewTicker(10 * time.Minute)
	ctx.NoDaemon = true
	first := true
	for {
		select {
		case c := <-call:
			if first {
				first = false
				log.SetWriter(nil)
				// TODO: Close stdout and stderr
			}
			if err := run(ctx, c); err != nil {
				return err
			}
			resetTimer(timeout, daemonTimeout)
		case <-ticker.C:
			if err := periodic(ctx); err != nil {
				return err
			}
		case <-timeout.C:
			return fmt.Errorf("daemon timeout")
		}
	}
}

// run executes a remote command.
func run(ctx *Ctx, r *daemon.Request) error {
	if r == nil {
		return fmt.Errorf("command channel closed")
	}
	defer close(r.Out)
	if _, ok := r.Cmd.(CallableCmd); !ok {
		return fmt.Errorf("received command: %v", r.Cmd)
	}
	// TODO: Recover
	cmd := r.Cmd.(CallableCmd)
	out, err := cmd.Call(ctx)
	r.Out <- &daemon.Response{out, err}
	return nil
}

// periodic performs regular maintenance tasks.
func periodic(ctx *Ctx) error {
	if ctx.UseOkta() {
		// TODO: Ignore temporary errors
		if err := ctx.Okta().RefreshSession(""); err != nil {
			return err
		}
	}
	// TODO: Refresh account information in batches
	return nil
}

// resetTimer configures t to fire after duration d.
func resetTimer(t *time.Timer, d time.Duration) {
	if !t.Stop() {
		<-t.C
	}
	t.Reset(d)
}
