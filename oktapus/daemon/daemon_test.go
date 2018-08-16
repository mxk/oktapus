package daemon

import (
	"encoding/gob"
	"io"
	"os"
	"os/exec"
	"strconv"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testAddr = Addr("127.0.0.1:0")

var skipAll bool

func TestIsNotRunning(t *testing.T) {
	defer func() { skipAll = t.Failed() }()

	start := time.Now()
	_, err := Addr("").Send(nil)
	assert.True(t, time.Since(start) < time.Second)

	assert.True(t, IsNotRunning(err))
	assert.False(t, IsNotRunning(nil))
	assert.False(t, IsNotRunning(io.EOF))
}

func TestKill(t *testing.T) {
	defer func() { skipAll = t.Failed() }()
	d, killFn := start(t, nil)
	assert.NotEqual(t, string(testAddr), string(d))
	killFn()

	start := time.Now()
	assert.NoError(t, d.Kill())
	assert.True(t, time.Since(start) < time.Second)

	_, err := d.Send(nil)
	assert.True(t, IsNotRunning(err))
}

func TestDaemonFd(t *testing.T) {
	d, kill := start(t, nil)
	defer kill()

	out, err := d.Send(nil)
	assert.NoError(t, err)
	assert.Nil(t, out)

	out, err = d.Send("hello, world")
	assert.NoError(t, err)
	assert.Equal(t, "hello, world", out)

	out, err = d.Send(123)
	assert.NoError(t, err)
	assert.Equal(t, 123, out)
}

func TestDaemonAddr(t *testing.T) {
	d, kill := start(t, echo)
	defer kill()

	out, err := d.Send(string(d))
	assert.NoError(t, err)
	assert.Equal(t, string(d), out)
}

func TestClose(t *testing.T) {
	d, kill := start(t, func(q *Request) { close(q.Rch) })
	defer kill()

	out, err := d.Send(nil)
	assert.Equal(t, io.EOF, err)
	assert.Nil(t, out)
}

type decErr string

func (e decErr) Error() string              { return string(e) }
func (e decErr) GobEncode() ([]byte, error) { return []byte(e), nil }
func (decErr) GobDecode(b []byte) error     { return decErr(b) }

type encErr string

func (e encErr) Error() string              { return string(e) }
func (e encErr) GobEncode() ([]byte, error) { return nil, e }

func TestGobError(t *testing.T) {
	d, kill := start(t, func(q *Request) { q.Rch <- encErr(q.Msg.(string)) })
	defer kill()

	gob.Register(decErr(""))
	assert.PanicsWithValue(t, "daemon decode: decErr", func() {
		d.Send(decErr("decErr"))
	})

	gob.Register(encErr(""))
	assert.PanicsWithValue(t, "daemon encode: encErr", func() {
		d.Send("encErr")
	})
}

type errMsg string

func (e errMsg) Error() string { return string(e) }

func TestDaemonError(t *testing.T) {
	d, kill := start(t, func(q *Request) { q.Rch <- errMsg(q.Msg.(string)) })
	defer kill()

	gob.Register(errMsg(""))
	out, err := d.Send("daemon error")
	assert.Equal(t, errMsg("daemon error"), err)
	assert.Nil(t, out)
}

func start(t *testing.T, fn func(q *Request)) (d Addr, kill func()) {
	if skipAll {
		t.Skip("unable to kill daemon")
	}
	d = testAddr
	term := make(chan struct{})
	require.NoError(t, d.Start(func(c *exec.Cmd) error {
		if f := c.ExtraFiles[0]; fn == nil {
			// f is about to be closed, so need to dup and update fdEnv
			fd, err := syscall.Dup(int(f.Fd()))
			require.NoError(t, err)
			os.Setenv(fdEnv, strconv.Itoa(fd))
			defer os.Unsetenv(fdEnv)
		} else {
			f.Close()
		}
		qch, err := d.Serve()
		require.NoError(t, err)
		if fn == nil {
			fn = echo
		}
		go daemon(qch, term, fn)
		return nil
	}))
	return d, func() {
		assert.NoError(t, d.Kill())
		select {
		case <-term:
		case <-time.After(time.Second):
			t.Fatal("daemon goroutine did not terminate")
		}
	}
}

func daemon(qch <-chan *Request, term chan<- struct{}, fn func(q *Request)) {
	defer close(term)
	timeout := time.After(ioTimeout + 1*time.Second)
	for {
		select {
		case q, ok := <-qch:
			if !ok {
				return
			}
			fn(q)
		case <-timeout:
			panic("daemon goroutine timeout")
		}
	}
}

func echo(q *Request) { q.Rch <- q.Msg }
