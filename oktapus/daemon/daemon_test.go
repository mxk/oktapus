package daemon

import (
	"encoding/gob"
	"os"
	"os/exec"
	"strconv"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsNotRunning(t *testing.T) {
	_, err := Send("", nil)
	assert.True(t, IsNotRunning(err))
	assert.False(t, IsNotRunning(nil))
}

func TestFdDaemon(t *testing.T) {
	addr, kill := runDaemon(t, true)
	defer kill()

	out, err := Send(addr, nil)
	assert.NoError(t, err)
	assert.Nil(t, out)

	out, err = Send(addr, "hello, world")
	assert.NoError(t, err)
	assert.Equal(t, "hello, world", out)

	out, err = Send(addr, 123)
	assert.NoError(t, err)
	assert.Equal(t, 123, out)
}

func TestAddrDaemon(t *testing.T) {
	addr, kill := runDaemon(t, false)
	defer kill()

	out, err := Send(addr, addr)
	assert.NoError(t, err)
	assert.Equal(t, addr, out)
}

type decErr string

func (e decErr) Error() string              { return string(e) }
func (e decErr) GobEncode() ([]byte, error) { return []byte(e), nil }
func (decErr) GobDecode(b []byte) error     { return decErr(b) }

func TestDecodeError(t *testing.T) {
	addr, kill := runDaemon(t, true)
	defer kill()

	gob.Register(decErr(""))
	out, err := Send(addr, decErr("fail"))
	assert.EqualError(t, err, "fail")
	assert.Nil(t, out)
}

func runDaemon(t *testing.T, useFd bool) (addr string, kill func()) {
	done := make(chan struct{})
	addr, err := Start("127.0.0.1:0", func(c *exec.Cmd) error {
		if s := c.ExtraFiles[0]; useFd {
			fd, err := syscall.Dup(int(s.Fd()))
			require.NoError(t, err)
			os.Setenv(fdEnv, strconv.Itoa(fd))
			defer os.Unsetenv(fdEnv)
		} else {
			s.Close()
		}
		qch, err := Serve(c.Args[len(c.Args)-1])
		require.NoError(t, err)
		go daemon(qch, done)
		return nil
	})
	require.NoError(t, err)
	return addr, func() {
		assert.NoError(t, Kill(addr))
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("daemon goroutine did not terminate")
		}
	}
}

func daemon(req <-chan *Request, done chan<- struct{}) {
	defer close(done)
	timeout := time.After(timeout + 1*time.Second)
	for {
		select {
		case q := <-req:
			if q == nil {
				return
			}
			q.Rch <- q.Msg
		case <-timeout:
			panic("daemon goroutine timeout")
		}
	}
}
