package daemon

import (
	"encoding/gob"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/LuminalHQ/oktapus/internal"
)

// fdEnv is an environment variable containing daemon's unix socket file
// descriptor.
var fdEnv = strings.ToUpper(internal.AppName) + "_DAEMON_FD"

// Addr returns the address of the daemon process for the given context.
func Addr(ctx Ctx) string {
	return filepath.Join(os.TempDir(), internal.AppName+"."+sig(ctx))
}

// Call executes cmd remotely and returns the result.
func Call(ctx Ctx, cmd interface{}, args []string) (interface{}, error) {
	cn := dialOrStart(ctx)
	defer cn.Close()
	err := gob.NewEncoder(cn).Encode(&Cmd{Cmd: cmd, Args: args})
	if err != nil {
		panic(err)
	}
	dec := gob.NewDecoder(cn)
	// TODO: Timeout
	for {
		var v interface{}
		if err := dec.Decode(&v); err != nil {
			panic(err)
		}
		switch v := v.(type) {
		case *Result:
			return v.Out, v.Err
		case *internal.LogMsg:
			log.Msg(v)
		default:
			panic(fmt.Sprintf("unexpected daemon response: %#v", v))
		}
	}
}

// Listen returns the channel on which the daemon receives incoming commands.
func Listen(addr string) <-chan *Cmd {
	var s net.Listener
	if v := os.Getenv(fdEnv); v != "" {
		fd, err := strconv.Atoi(v)
		if err != nil {
			panic(err)
		}
		s, err = net.FileListener(os.NewFile(uintptr(fd), addr))
		if err != nil {
			panic(err)
		}
	} else {
		s = listenUnix(addr)
	}
	ch := make(chan *Cmd)
	go func() {
		defer close(ch)
		for {
			cn, err := s.Accept()
			if err != nil {
				panic(err)
			} else if !serve(cn, ch) {
				return
			}
		}
	}()
	return ch
}

// Kill terminates daemons. If active is true, the daemon at Addr() is killed.
// If others is true, all other daemons are killed.
func Kill(ctx Ctx, active, others bool) {
	var addrs []string
	act := Addr(ctx)
	if active {
		addrs = append(addrs, act)
	}
	if others {
		ext := filepath.Ext(act)
		match, err := filepath.Glob(act[:len(act)-len(ext)] + ".*")
		if err != nil {
			panic(err)
		}
		addrs = append(addrs, match...)
	}
	for _, addr := range addrs {
		if addr == act {
			if !active {
				continue
			}
			active = false
		}
		raddr, err := net.ResolveUnixAddr("unix", addr)
		if err != nil {
			panic(err)
		}
		name := filepath.Ext(addr)[1:]
		cn, err := net.DialUnix("unix", nil, raddr)
		if err != nil {
			log.W("Error dialing daemon %q: %v", name, err)
			continue
		}
		if err = gob.NewEncoder(cn).Encode(&Cmd{Cmd: "quit"}); err != nil {
			log.W("Error killing daemon %q: %v", name, err)
		}
		cn.Close()
	}
}

// dialOrStart connects to the daemon process, starting a new one if needed.
func dialOrStart(ctx Ctx) *net.UnixConn {
	addr := Addr(ctx)
	raddr, err := net.ResolveUnixAddr("unix", addr)
	if err != nil {
		panic(err)
	}
	cn, err := net.DialUnix("unix", nil, raddr)
	if err == nil {
		return cn
	}
	e, exist := err.(*net.OpError)
	if !exist {
		panic(err)
	}
	sc, ok := e.Err.(*os.SyscallError)
	if os.IsNotExist(e.Err) || (ok && sc.Err == syscall.ECONNREFUSED) {
		os.Remove(addr)
		start(ctx, addr)
		if cn, err = net.DialUnix("unix", nil, raddr); err == nil {
			return cn
		}
	}
	panic(err)
}

// start creates a new daemon process.
func start(ctx Ctx, addr string) {
	c := ctx.DaemonCmd(addr)
	s := listenUnix(addr)
	s.SetUnlinkOnClose(false)
	defer s.Close()
	f, err := s.File()
	if err != nil {
		panic(err)
	}
	defer f.Close()
	if c.SysProcAttr == nil {
		c.SysProcAttr = new(syscall.SysProcAttr)
	}
	c.SysProcAttr.Setpgid = true
	c.ExtraFiles = append(c.ExtraFiles, f)
	c.Env = append(c.Env, fmt.Sprintf("%s=%d", fdEnv, 2+len(c.ExtraFiles)))
	if err = c.Start(); err != nil {
		panic(err)
	}
}

// serve handles a single connection request.
func serve(cn net.Conn, ch chan<- *Cmd) bool {
	defer cn.Close()
	ok, enc := true, gob.NewEncoder(cn)
	prev := log.SetFunc(func(m *internal.LogMsg) {
		if ok {
			v := interface{}(m)
			if err := enc.Encode(&v); err != nil {
				ok = false
			}
		}
	})
	defer log.SetFunc(prev)
	c := new(Cmd)
	if err := gob.NewDecoder(cn).Decode(c); err != nil {
		log.E("Call decode error: %v", err)
		return false
	}
	// TODO: Timeouts
	out := make(chan *Result)
	c.Out = out
	ch <- c
	r := <-out
	if r == nil {
		log.E("Call result not returned")
		return false
	}
	if r.Err != nil {
		r.Err = internal.EncodableError(r.Err)
	}
	v := interface{}(r)
	if err := enc.Encode(&v); err != nil {
		log.E("Call encode error: %v", err)
		ok = false
	}
	return ok
}

// listenUnix creates a new unix listening socket.
func listenUnix(addr string) *net.UnixListener {
	laddr, err := net.ResolveUnixAddr("unix", addr)
	if err != nil {
		panic(err)
	}
	s, err := net.ListenUnix("unix", laddr)
	if err != nil {
		panic(err)
	}
	return s
}
