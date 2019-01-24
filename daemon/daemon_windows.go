package daemon

import (
	"errors"
	"net"
	"os"
	"os/exec"
	"syscall"
	"time"
)

func startDaemon(l *net.TCPListener, fn StartFunc, c *exec.Cmd) error {
	// Windows does not support ExtraFiles or FileListener, so close the socket
	// and wait for the daemon to re-open it.
	// https://github.com/golang/go/issues/9503
	// https://github.com/golang/go/issues/10350
	// https://github.com/golang/go/issues/21085
	addr := l.Addr().String()
	l.Close()
	if err := fn(c); err != nil {
		return err
	}
	wait := make(chan error, 1)
	if c.Process != nil {
		go func() { wait <- c.Wait() }()
	}
	stop := time.Now().Add(3 * time.Second)
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	var dialErr error
	for {
		select {
		case t := <-tick.C:
			if timeout := stop.Sub(t); timeout > 50*time.Millisecond {
				cn, err := net.DialTimeout("tcp", addr, timeout)
				if err == nil {
					cn.Close()
					return nil
				} else if isNotRunning(err) {
					continue
				}
				dialErr = err
			} else {
				dialErr = syscall.ETIMEDOUT
			}
			tick.Stop()
			if c.Process != nil {
				c.Process.Kill()
			} else {
				close(wait)
			}
		case err := <-wait:
			if err == nil {
				if dialErr != nil {
					err = dialErr
				} else {
					err = errors.New("daemon: unexpected daemon termination")
				}
			}
			return err
		}
	}
}

func isNotRunning(err error) bool {
	const WSAECONNREFUSED syscall.Errno = 10061
	if e, ok := err.(*net.OpError); ok {
		se, ok := e.Err.(*os.SyscallError)
		return ok && se.Err == WSAECONNREFUSED
	}
	return false
}

func dup(_ *os.File) int { panic("not supported") }
