package daemon

import (
	"encoding/gob"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"github.com/LuminalHQ/cloudcover/oktapus/internal"
)

const (
	fdEnv      = "OKTAPUS_DAEMON_FD"
	sockPrefix = "oktapus."
)

var (
	callTimeout   = 15 * time.Second
	keepaliveRate = 10 * time.Second
)

// msg contains either a log message or a response from the daemon. An empty msg
// is a keepalive.
type msg struct {
	Msg *internal.LogMsg
	Rsp *Response
}

// Addr returns the address of the daemon process for the given context.
func Addr(ctx Ctx) string {
	m := ctx.EnvMap()
	return filepath.Join(os.TempDir(), sockPrefix+sig(m))
}

// Call executes cmd remotely and returns the result.
func Call(ctx Ctx, cmd interface{}) (interface{}, error) {
	cn := dialOrStart(ctx)
	defer cn.Close()
	err := gob.NewEncoder(cn).Encode(&cmd)
	if err != nil {
		panic(err)
	}
	dec := gob.NewDecoder(cn)
	for {
		cn.SetReadDeadline(internal.Time().Add(callTimeout))
		var m msg
		if err := dec.Decode(&m); err != nil {
			panic(err)
		}
		if m.Msg != nil {
			log.Msg(m.Msg)
		}
		if m.Rsp != nil {
			return m.Rsp.Out, m.Rsp.Err
		}
	}
}

// Listen returns the channel on which the daemon receives incoming commands.
func Listen(addr string) <-chan *Request {
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
	ch := make(chan *Request)
	go func() {
		defer close(ch)
		for {
			cn, err := s.Accept()
			if err != nil {
				panic(err)
			}
			serve(cn, ch)
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
		sig := filepath.Ext(addr)[1:]
		cn, err := net.DialUnix("unix", nil, raddr)
		if err != nil {
			if e, ok := err.(*net.OpError); !ok || !os.IsNotExist(e.Err) {
				log.W("Error dialing daemon %q: %v", sig, err)
			}
			continue
		}
		v := interface{}("quit")
		if err = gob.NewEncoder(cn).Encode(&v); err != nil {
			log.W("Error killing daemon %q: %v", sig, err)
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
	e, ok := err.(*net.OpError)
	if !ok {
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
	path, err := exec.LookPath(os.Args[0])
	if err != nil {
		panic(err)
	}
	s := listenUnix(addr)
	defer s.Close()
	f, err := s.File()
	if err != nil {
		panic(err)
	}
	defer f.Close()
	name := filepath.Base(path)
	c := exec.Cmd{
		Path:        name,
		Args:        []string{name, "daemon", addr},
		Env:         append(os.Environ(), fdEnv+"=3"),
		Dir:         filepath.Dir(path),
		Stdout:      os.Stdout,
		Stderr:      os.Stderr,
		ExtraFiles:  []*os.File{f},
		SysProcAttr: &syscall.SysProcAttr{Setpgid: true},
	}
	if err = ctx.StartDaemon(&c); err != nil {
		panic(err)
	}
	s.SetUnlinkOnClose(false)
}

// serve handles a single connection request.
func serve(cn net.Conn, ch chan<- *Request) {
	defer cn.Close()
	ok, enc := true, gob.NewEncoder(cn)
	prev := log.SetFunc(func(m *internal.LogMsg) {
		if ok {
			if err := enc.Encode(msg{Msg: m}); err != nil {
				ok = false
			}
		}
	})
	defer log.SetFunc(prev)
	out := make(chan *Response)
	req := &Request{Out: out}
	if err := gob.NewDecoder(cn).Decode(&req.Cmd); err != nil {
		log.E("Request decode error: %v", err)
		return
	}
	keepalive := time.NewTicker(keepaliveRate)
	defer keepalive.Stop()
	for {
		select {
		case ch <- req:
			ch = nil
		case rsp := <-out:
			if rsp != nil {
				rsp.Err = internal.RegisteredError(rsp.Err)
				if err := enc.Encode(msg{Rsp: rsp}); err != nil {
					log.E("Response encode error: %v", err)
				}
			}
			return
		case <-keepalive.C:
			if err := enc.Encode(msg{}); err != nil {
				log.E("Keepalive encode error: %v", err)
				keepalive.Stop()
			}
		}
	}
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
