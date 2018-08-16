package daemon

import (
	"encoding/gob"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"time"
)

// DefaultAddr is the default daemon listening address.
const DefaultAddr = "127.0.0.1:1271"

const (
	fdEnv   = "OKTAPUS_DAEMON_FD"
	timeout = 5 * time.Second
)

func init() {
	gob.Register(kill{})
	gob.Register(daemonError(""))
}

type kill struct{}
type daemonError string

// Error implements the error interface.
func (e daemonError) Error() string { return string(e) }

// StartFunc should call c.Start() to start the daemon process.
type StartFunc func(c *exec.Cmd) error

// Start starts the daemon process and returns the listening socket address.
func Start(addr string, start StartFunc) (string, error) {
	path, err := filepath.Abs(os.Args[0])
	if err != nil {
		return "", err
	}
	s, err := listen(addr)
	if err != nil {
		return "", err
	}
	defer s.Close()
	f, err := s.File()
	if err != nil {
		return "", err
	}
	defer f.Close()
	addr = s.Addr().String()
	s.Close()
	return addr, start(initCmd(&exec.Cmd{
		Path:       path,
		Args:       []string{filepath.Base(path), "daemon", addr},
		Env:        append(os.Environ(), fdEnv+"=3"),
		Dir:        filepath.Dir(path),
		Stdout:     os.Stdout,
		Stderr:     os.Stderr,
		ExtraFiles: []*os.File{f},
	}))
}

// Request contains the message received by the daemon and the response channel.
type Request struct {
	Msg interface{}
	Rch chan<- interface{}
}

// Serve returns the channel on which the daemon will send incoming messages.
func Serve(addr string) (<-chan *Request, error) {
	var s net.Listener
	var err error
	// TODO: Windows (https://github.com/golang/go/issues/21085)
	if v, ok := os.LookupEnv(fdEnv); ok {
		var fd int
		if fd, err = strconv.Atoi(v); err != nil {
			return nil, err
		}
		s, err = net.FileListener(os.NewFile(uintptr(fd), addr))
	} else {
		s, err = listen(addr)
	}
	if err != nil {
		return nil, err
	}
	qch := make(chan *Request)
	go accept(s, qch)
	return qch, nil
}

// Send sends a message to the daemon and returns the response.
func Send(addr string, msg interface{}) (interface{}, error) {
	c, err := net.DialTimeout("tcp", fixAddr(addr), timeout)
	if err != nil {
		return nil, err
	}
	defer c.Close()
	c.SetDeadline(time.Now().Add(timeout))
	if err = gob.NewEncoder(c).Encode(&msg); err != nil {
		return nil, err
	}
	var rsp interface{}
	if err = gob.NewDecoder(c).Decode(&rsp); err != nil {
		rsp = nil
	} else if e, ok := rsp.(daemonError); ok {
		rsp = nil
		err = e
	}
	return rsp, err
}

// Kill terminates the daemon.
func Kill(addr string) error {
	_, err := Send(addr, kill{})
	if err == io.EOF {
		err = nil
	}
	return err
}

// IsNotRunning returns true if err indicates that the daemon is not running.
func IsNotRunning(err error) bool {
	return isNotRunning(err)
}

func fixAddr(addr string) string {
	if addr == "" {
		addr = DefaultAddr
	} else if addr[0] == ':' {
		addr = "127.0.0.1" + addr
	}
	return addr
}

func listen(addr string) (*net.TCPListener, error) {
	s, err := net.Listen("tcp", fixAddr(addr))
	if err != nil {
		return nil, err
	}
	return s.(*net.TCPListener), nil
}

func accept(s net.Listener, qch chan<- *Request) {
	defer func() {
		close(qch)
		s.Close()
	}()
	for {
		if c, err := s.Accept(); err != nil {
			panic(err)
		} else if !serve(&netConn{Conn: c}, qch) {
			return
		}
	}
}

func serve(c *netConn, qch chan<- *Request) bool {
	defer c.Close()
	c.SetDeadline(time.Now().Add(timeout))

	// Receive
	var msg, rsp interface{}
	err := gob.NewDecoder(c).Decode(&msg)
	if err != nil {
		if c.err != nil {
			return true
		}
		rsp = daemonError(err.Error())
	} else if _, ok := msg.(kill); ok {
		return false
	}

	// Respond
	if rsp == nil {
		rch := make(chan interface{})
		qch <- &Request{msg, rch} // TODO: Timeout?
		rsp = <-rch
	}
	if err = gob.NewEncoder(c).Encode(&rsp); err != nil && c.err == nil {
		panic(err)
	}
	return true
}

// netConn is used to distinguish network errors, which can be ignored, from gob
// encode/decode errors, which must be reported.
type netConn struct {
	net.Conn
	err error
}

func (c *netConn) Read(b []byte) (n int, err error) {
	if n, err = c.Conn.Read(b); err != nil {
		c.err = err
	}
	return
}

func (c *netConn) Write(b []byte) (n int, err error) {
	if n, err = c.Conn.Write(b); err != nil {
		c.err = err
	}
	return
}
