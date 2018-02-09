package cmd

import (
	"fmt"
	"os"
	"time"

	"github.com/LuminalHQ/oktapus/daemon"
	"github.com/LuminalHQ/oktapus/op"
)

func init() {
	op.Register(&op.CmdInfo{
		Names:   []string{"daemon"},
		Summary: "Persistent daemon process",
		Usage:   "[options] addr",
		MinArgs: 1,
		MaxArgs: 1,
		Hidden:  true,
		New:     func() op.Cmd { return daemonCmd{Name: "daemon"} },
	})
}

type daemonCmd struct {
	Name
	noFlags
}

func (daemonCmd) Run(ctx *op.Ctx, args []string) error {
	addr := args[0]
	call := daemon.Listen(addr)
	if _, err := os.Stat(addr); err == nil {
		defer os.Remove(addr)
	}
	const daemonTimeout = 36 * time.Hour
	timeout := time.NewTimer(daemonTimeout)
	ticker := time.NewTicker(10 * time.Minute)
	ctx.UseDaemon = false
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
func run(ctx *op.Ctx, r *daemon.Request) error {
	// TODO: Intercept log.F calls?
	if r == nil {
		return fmt.Errorf("command channel closed")
	}
	defer close(r.Out)
	if _, ok := r.Cmd.(op.CallableCmd); !ok {
		return fmt.Errorf("received command: %v", r.Cmd)
	}
	// TODO: Recover
	cmd := r.Cmd.(op.CallableCmd)
	out, err := cmd.Call(ctx)
	r.Out <- &daemon.Response{Out: out, Err: err}
	return nil
}

// periodic performs regular maintenance tasks.
func periodic(ctx *op.Ctx) error {
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