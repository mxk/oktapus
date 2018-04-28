package internal

import (
	"os/exec"
	"syscall"
)

// ExitCode returns the exit code from *exec.ExitError. It returns -1 if the
// code could not be determined.
func ExitCode(err error) int {
	if err == nil {
		return 0
	} else if e, ok := err.(*exec.ExitError); ok {
		return e.Sys().(syscall.WaitStatus).ExitStatus()
	}
	return -1
}
