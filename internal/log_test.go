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
	l := &log{w: &buf}
	l.D("debug")
	eq("[D] debug\n")
	l.I("info")
	eq("[I] info\n")
	l.W("warning")
	eq("[W] warning\n")
	l.E("error")
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

func TestLogWriter(t *testing.T) {
	var buf bytes.Buffer
	prev := Log.SetWriter(&buf)
	require.True(t, prev == os.Stderr)
	Log.I("writer")
	assert.Equal(t, "[I] writer\n", buf.String())
	assert.True(t, Log.SetWriter(prev) == &buf)
}

func TestLogFunc(t *testing.T) {
	var buf bytes.Buffer
	Log.SetWriter(nil)
	Log.SetFunc(func(m *LogMsg) {
		buf.WriteByte(m.Level)
		buf.WriteByte(' ')
		buf.WriteString(m.Msg)
	})
	Log.I("func")
	assert.Equal(t, "I func\n", buf.String())
	buf.Reset()
	Log.SetFunc(nil)
	Log.I("nil")
	assert.Empty(t, buf.String())
}
