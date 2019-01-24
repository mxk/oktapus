// +build darwin dragonfly freebsd linux netbsd openbsd solaris

package daemon

import (
	"net"
	"os"
	"os/exec"
	"syscall"
)

func startDaemon(l *net.TCPListener, fn StartFunc, c *exec.Cmd) error {
	f, err := l.File()
	if err != nil {
		return err
	}
	defer f.Close()

	// Ensure that only one socket descriptor remains open for fn
	l.Close()
	c.Env = append(c.Env, fdEnv+"=3")
	c.ExtraFiles = []*os.File{f}

	// Create a new process group to avoid receiving signals
	c.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	return fn(c)
}

func isNotRunning(err error) bool {
	if e, ok := err.(*net.OpError); ok {
		se, ok := e.Err.(*os.SyscallError)
		return ok && se.Err == syscall.ECONNREFUSED
	}
	return false
}

func dup(f *os.File) int {
	fd, err := syscall.Dup(int(f.Fd()))
	if err != nil {
		panic(err)
	}
	return fd
}
