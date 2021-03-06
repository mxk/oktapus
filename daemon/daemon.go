package daemon

import (
	"bufio"
	"encoding/gob"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"time"
)

const (
	fdEnv       = "OKTAPUS_DAEMON_FD"
	dialTimeout = 3 * time.Second
	ioTimeout   = 5 * time.Second
)

func init() {
	gob.Register(gobError(""))
	gob.Register(kill{})
}

// DefaultAddr is the default daemon listening address.
const DefaultAddr = Addr("127.0.0.1:1271")

// IsNotRunning returns true if err indicates that the daemon is not running.
func IsNotRunning(err error) bool { return isNotRunning(err) }

// Addr is the daemon network address. The zero value implies DefaultAddr.
type Addr string

// StartFunc should call c.Start() to start the daemon process after making any
// adjustments to the command parameters.
type StartFunc func(c *exec.Cmd) error

// Request contains the client connection, the received message, and the
// response channel. The handler must either send a response via Rch or close
// it without sending anything, which will close the network connection.
type Request struct {
	net.Conn
	Msg interface{}
	Rch chan<- interface{}
}

// Start starts the daemon process. Address is updated to reflect the actual
// listening socket address.
func (d *Addr) Start(fn StartFunc) error {
	path, err := exec.LookPath(os.Args[0])
	if err != nil {
		return err
	}
	l, err := d.listen()
	if err != nil {
		return err
	}
	defer l.Close()
	if fn == nil {
		fn = func(c *exec.Cmd) error { return c.Start() }
	}
	return startDaemon(l, fn, &exec.Cmd{
		Path:   path,
		Args:   []string{filepath.Base(path), "daemon", string(*d)},
		Env:    os.Environ(),
		Dir:    filepath.Dir(path),
		Stdout: os.Stdout,
		Stderr: os.Stderr,
	})
}

// Serve returns the channel on which the daemon will send incoming messages.
// Address is updated to reflect the actual listening socket address.
func (d *Addr) Serve() (<-chan *Request, error) {
	var l *net.TCPListener
	var err error
	if v, ok := os.LookupEnv(fdEnv); ok && runtime.GOOS != "windows" {
		var fd int
		if fd, err = strconv.Atoi(v); err == nil {
			l, err = d.inherit(fd)
		}
	} else {
		l, err = d.listen()
	}
	if err != nil {
		return nil, err
	}
	qch := make(chan *Request)
	go accept(l, qch)
	return qch, nil
}

// Send sends a message to the daemon and returns the response. The message and
// response data types must be gob-registered.
func (d Addr) Send(msg interface{}) (interface{}, error) {
	c, err := net.DialTimeout("tcp", d.addr(), dialTimeout)
	if err != nil {
		return nil, err
	}
	defer c.Close()
	c.SetDeadline(time.Now().Add(ioTimeout))
	if err = newEncoder(c).Encode(&msg); err != nil {
		return nil, err
	}
	var rsp interface{}
	if err = gob.NewDecoder(c).Decode(&rsp); err == nil {
		switch v := rsp.(type) {
		case gobError:
			panic(string(v))
		case error:
			rsp = nil
			err = v
		}
	} else {
		rsp = nil
	}
	return rsp, err
}

// Kill terminates the daemon.
func (d Addr) Kill() error {
	_, err := d.Send(kill{})
	if err == io.EOF || IsNotRunning(err) {
		err = nil
	}
	return err
}

func (d Addr) addr() string {
	if d == "" {
		d = DefaultAddr
	} else if d[0] == ':' {
		d = "127.0.0.1" + d
	}
	return string(d)
}

func (d *Addr) listen() (*net.TCPListener, error) {
	l, err := net.Listen("tcp", d.addr())
	if err != nil {
		return nil, err
	}
	*d = Addr(l.Addr().String())
	return l.(*net.TCPListener), nil
}

func (d *Addr) inherit(fd int) (*net.TCPListener, error) {
	f := os.NewFile(uintptr(fd), string(*d))
	defer f.Close()
	l, err := net.FileListener(f)
	if err != nil {
		return nil, err
	}
	*d = Addr(l.Addr().String())
	return l.(*net.TCPListener), nil
}

func accept(l *net.TCPListener, qch chan<- *Request) {
	for {
		c, err := l.Accept()
		if err != nil {
			panic(err)
		}
		if c := (conn{Conn: c}); c.serve(qch) {
			c.Close()
			continue
		}
		l.Close()
		c.Close()
		close(qch)
		return
	}
}

// Internal message types.
type (
	kill     struct{}
	gobError string
)

func (e gobError) Error() string { return string(e) }

// conn handles requests and distinguishes network errors, which the daemon can
// ignore, from gob encode/decode errors, which are reported to the client.
type conn struct {
	net.Conn
	err   error
	dirty bool
}

func (c *conn) Read(b []byte) (n int, err error) {
	if n, err = c.Conn.Read(b); err != nil {
		c.err = err
	}
	return
}

func (c *conn) Write(b []byte) (n int, err error) {
	c.dirty = true
	if n, err = c.Conn.Write(b); err != nil {
		c.err = err
	}
	return
}

func (c *conn) isGobError(err error) bool { return err != nil && c.err == nil }

func (c *conn) serve(qch chan<- *Request) bool {
	c.SetDeadline(time.Now().Add(ioTimeout))

	// Receive
	var msg, rsp interface{}
	err := gob.NewDecoder(c).Decode(&msg)
	if err != nil {
		if !c.isGobError(err) {
			return true
		}
		rsp = gobError("daemon decode: " + err.Error())
	} else if _, ok := msg.(kill); ok {
		return false
	}

	// Handle
	if rsp == nil {
		rch := make(chan interface{})
		qch <- &Request{c, msg, rch} // TODO: Timeout?
		var ok bool
		if rsp, ok = <-rch; !ok {
			return true
		}
	}

	// Respond
	enc := newEncoder(c)
	if err = enc.Encode(&rsp); c.isGobError(err) {
		if !c.dirty {
			enc.b.Reset(c)
			rsp = gobError("daemon encode: " + err.Error())
			err = enc.Encode(&rsp)
		}
		if c.isGobError(err) {
			panic(err)
		}
	}
	return true
}

// encoder adds a buffer between gob.Encoder and io.Writer to increase the
// chances of detecting encoder errors before any data is sent. Normally, the
// encoder writes out type information before calling GobEncode, which prevents
// the daemon from sending a gobError back to the client if GobEncode fails.
type encoder struct {
	e *gob.Encoder
	b *bufio.Writer
}

func newEncoder(w io.Writer) encoder {
	b := bufio.NewWriter(w)
	return encoder{gob.NewEncoder(b), b}
}

func (e encoder) Encode(v interface{}) error {
	err := e.e.Encode(v)
	if err == nil {
		err = e.b.Flush()
	}
	return err
}
