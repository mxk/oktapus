package internal

import (
	"bytes"
	"os"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLog(t *testing.T) {
	var buf bytes.Buffer
	eq := func(want string) {
		assert.Equal(t, want, buf.String())
		buf.Reset()
	}
	Log := log{&buf}
	Log.D("debug")
	eq("[D] debug\n")
	Log.I("info")
	eq("[I] info\n")
	Log.W("warning")
	eq("[W] warning\n")
	Log.E("error")
	eq("[E] error\n")
}

func TestLogFatal(t *testing.T) {
	if os.Getenv("TEST_CHILD") == "1" {
		Log.F("fatal")
		panic("fail")
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestLogFatal")
	cmd.Env = append(os.Environ(), "TEST_CHILD=1")

	out, err := cmd.CombinedOutput()
	e, ok := err.(*exec.ExitError)
	require.True(t, ok)
	require.False(t, e.Success())
	assert.Equal(t, "[F] fatal\n", string(out))
}
