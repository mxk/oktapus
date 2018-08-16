// +build darwin dragonfly freebsd linux netbsd openbsd solaris

package daemon

import (
	"net"
	"os"
	"os/exec"
	"syscall"
)

func initCmd(c *exec.Cmd) *exec.Cmd {
	// Create a new process group to avoid receiving signals
	c.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	return c
}

func isNotRunning(err error) bool {
	if e, ok := err.(*net.OpError); ok {
		se, ok := e.Err.(*os.SyscallError)
		return ok && se.Err == syscall.ECONNREFUSED
	}
	return false
}
