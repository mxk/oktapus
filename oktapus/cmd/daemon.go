package cmd

import (
	"log"
	"os"
	"reflect"

	"github.com/mxk/cloudcover/oktapus/daemon"
	"github.com/mxk/cloudcover/oktapus/op"
	"github.com/mxk/cloudcover/x/cli"
)

var daemonCli = cli.Main.Add(&cli.Info{
	Name:    "daemon",
	Usage:   "[options] [addr]",
	Summary: "Persistent daemon process",
	MaxArgs: 1,
	Hide:    true,
	New:     func() cli.Cmd { return &daemonCmd{} },
})

type daemonCmd struct {
	V bool `flag:"Verbose logging"`

	addr  daemon.Addr
	saved map[string]*op.SavedCtx
}

func (*daemonCmd) Info() *cli.Info { return daemonCli }

func (d *daemonCmd) Main(args []string) error {
	d.addr = daemonAddr(args)
	os.Setenv(op.DaemonEnv, "")
	qch, err := d.addr.Serve()
	if err != nil {
		return err
	}
	d.log("Daemon listening on:", string(d.addr))
	d.saved = make(map[string]*op.SavedCtx)
	for {
		// TODO: SavedCtx timeout
		// TODO: Periodic tasks
		select {
		case q, ok := <-qch:
			if !ok {
				d.log("Kill command received")
				return nil
			}
			d.log("Connection from:", q.RemoteAddr())
			if !d.serve(q) {
				// TODO: Graceful reload with new process inheriting flags,
				// arguments, and network/stdio file descriptors.
				return nil
			}
		}
	}
}

func (d *daemonCmd) serve(q *daemon.Request) bool {
	defer close(q.Rch)

	// Check message type and version
	type ver interface{ Version() op.Ver }
	if v, ok := q.Msg.(ver); !ok {
		typ := "nil"
		if t := reflect.TypeOf(q.Msg); t != nil {
			typ = t.String()
		}
		d.log("Invalid message type:", typ)
		return true
	} else if v := v.Version(); v != op.CtxVer {
		d.logf("Incompatible type version: %v (expecting %v)", v, op.CtxVer)
		return false
	}

	// Handle request
	switch v := q.Msg.(type) {
	case *op.GetCtx:
		if sc := d.saved[v.Sig]; sc != nil {
			d.log("Context found:", v.Sig)
			q.Rch <- sc
		} else {
			d.log("Context not found:", v.Sig)
		}
	case *op.SavedCtx:
		d.log("Context updated:", v.Sig)
		d.saved[v.Sig] = v
	}
	return true
}

func (d *daemonCmd) log(v ...interface{}) {
	if d.V {
		log.Println(v...)
	}
}

func (d *daemonCmd) logf(format string, v ...interface{}) {
	if d.V {
		log.Printf(format, v...)
	}
}

func daemonAddr(args []string) daemon.Addr {
	if len(args) > 0 {
		return daemon.Addr(args[0])
	}
	return daemon.Addr(os.Getenv(op.DaemonEnv))
}
