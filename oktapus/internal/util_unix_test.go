// +build darwin dragonfly freebsd linux netbsd openbsd solaris

package internal

import (
	"errors"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestExitCode(t *testing.T) {
	assert.Equal(t, -1, ExitCode(errors.New("")))
	assert.Equal(t, 0, ExitCode(exec.Command("true").Run()))
	assert.Equal(t, 1, ExitCode(exec.Command("false").Run()))
	assert.Equal(t, 2, ExitCode(exec.Command("sh", "-c", "exit 2").Run()))
}
