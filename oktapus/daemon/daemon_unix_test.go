package daemon

import (
	"bytes"
	"crypto/rand"
	"crypto/sha512"
	"encoding/base64"
	"encoding/gob"
	"encoding/hex"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
	"time"

	"github.com/LuminalHQ/cloudcover/oktapus/internal"
	"github.com/LuminalHQ/cloudcover/x/fast"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAddr(t *testing.T) {
	ctx := newTestCtx()
	h := sha512.Sum512([]byte("RAND=" + ctx.env["RAND"] + "\n"))
	sig := base64.URLEncoding.EncodeToString(h[:12])
	want := filepath.Join(os.TempDir(), sockPrefix+sig)
	assert.Equal(t, want, Addr(ctx))
}

func TestFdDaemon(t *testing.T) {
	ctx := newTestCtx()
	out, err := Call(ctx, "fd")
	require.NoError(t, err)
	assert.Equal(t, "fd", out)
	ctx.Close(t)
	ctx.Wait(t)
}

func TestAddrDaemon(t *testing.T) {
	ctx := newTestCtx()
	ctx.StartDaemon(nil)
	out, err := Call(ctx, "addr")
	require.NoError(t, err)
	assert.Equal(t, "addr", out)
	ctx.Close(t)
	ctx.Wait(t)
}

func TestKill(t *testing.T) {
	ctx := newTestCtx()
	out, err := Call(ctx, nil)
	require.NoError(t, err)
	assert.Nil(t, out)
	Kill(ctx, true, true)
	ctx.Wait(t)
}

func TestLog(t *testing.T) {
	ctx := newTestCtx()
	buf := new(bytes.Buffer)
	prev := log.SetWriter(buf)
	defer log.SetWriter(prev)
	cmd := []*internal.LogMsg{
		{Level: 'H', Msg: "hello"},
		{Level: 'W', Msg: "world"},
	}
	gob.Register(cmd)
	out, err := Call(ctx, cmd)
	require.NoError(t, err)
	want := "[H] hello\n[W] world\n"
	assert.Equal(t, want, buf.String())
	assert.Equal(t, want, out.(string))
	ctx.Close(t)
	ctx.Wait(t)
}

func TestKeepalive(t *testing.T) {
	stop := fastTime()
	defer func(t, r time.Duration) {
		close(stop)
		callTimeout, keepaliveRate = t, r
	}(callTimeout, keepaliveRate)

	sleep := 80 * time.Millisecond
	callTimeout = 40 * time.Millisecond
	keepaliveRate = 10 * time.Millisecond
	ctx := newTestCtx()
	gob.Register(sleep)
	out, err := Call(ctx, sleep)
	require.NoError(t, err)
	assert.Equal(t, sleep, out)
	ctx.Close(t)
	ctx.Wait(t)

	keepaliveRate = time.Second
	ctx = newTestCtx()
	defer func() {
		ctx.Close(t)
		ctx.Wait(t)
		p := recover()
		e, ok := p.(*net.OpError)
		require.True(t, ok)
		assert.True(t, e.Timeout())
	}()
	out, err = Call(ctx, -sleep)
}

type testCtx struct {
	env  map[string]string
	sock string
	quit chan<- struct{}
	done <-chan struct{}
}

func newTestCtx() *testCtx {
	var b [sha512.BlockSize]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	s := sha512.Sum512(b[:])
	return &testCtx{env: map[string]string{"RAND": hex.EncodeToString(s[:])}}
}

func (ctx *testCtx) EnvMap() map[string]string {
	return ctx.env
}

func (ctx *testCtx) StartDaemon(c *exec.Cmd) error {
	quit, done := make(chan struct{}), make(chan struct{})
	ctx.quit, ctx.done = quit, done
	addr, fd := ctx.sock, -1
	if c == nil {
		ctx.sock = Addr(ctx)
		addr = ctx.sock
	} else {
		ctx.sock = c.Args[len(c.Args)-1]
		addr = ""
		var err error
		if fd, err = syscall.Dup(int(c.ExtraFiles[0].Fd())); err != nil {
			panic(err)
		}
		os.Setenv(fdEnv, strconv.Itoa(fd))
		defer os.Unsetenv(fdEnv)
	}
	go echoDaemon(ctx.sock, fd, quit, done, Listen(addr))
	return nil
}

func (ctx *testCtx) Close(t *testing.T) {
	_, err := os.Stat(ctx.sock)
	assert.NoError(t, err)
	close(ctx.quit)
}

func (ctx *testCtx) Wait(t *testing.T) {
	select {
	case <-ctx.done:
		_, err := os.Stat(ctx.sock)
		assert.True(t, os.IsNotExist(err))
	case <-time.After(time.Second):
		t.Error("daemon goroutine did not terminate")
	}
}

func echoDaemon(sock string, fd int, quit <-chan struct{}, done chan<- struct{}, call <-chan *Request) {
	defer close(done)
	if sock != "" {
		defer os.Remove(sock)
	}
	if fd != -1 {
		defer syscall.Close(fd)
	}
	timeout := time.After(3 * time.Second)
	for {
		select {
		case r := <-call:
			if !echo(r) {
				return
			}
		case <-quit:
			return
		case <-timeout:
			panic("daemon goroutine timeout")
		}
	}
}

func echo(r *Request) bool {
	defer close(r.Out)
	switch cmd := r.Cmd.(type) {
	case string:
		if cmd == "quit" {
			return false
		}
	case []*internal.LogMsg:
		buf := new(bytes.Buffer)
		rlog := internal.NewLogger(buf, log.SetFunc(nil))
		for _, m := range cmd {
			rlog.Msg(m)
		}
		r.Cmd = buf.String()
	case time.Duration:
		if cmd < 0 {
			time.Sleep(-cmd)
			return false
		}
		time.Sleep(cmd)
	}
	r.Out <- &Response{r.Cmd, nil}
	return true
}

func fastTime() chan<- struct{} {
	fast.MockTime(time.Now())
	stop := make(chan struct{})
	tick := time.NewTicker(time.Millisecond)
	go func() {
		defer func() {
			tick.Stop()
			fast.MockTime(time.Time{})
		}()
		for {
			select {
			case t := <-tick.C:
				fast.MockTime(t)
			case <-stop:
				return
			}
		}
	}()
	return stop
}
