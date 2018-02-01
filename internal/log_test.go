package internal

import (
	"bytes"
	"os"
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
	l := new(log)
	exitCode := 0
	l.SetWriter(&buf)
	l.SetExitFunc(func(code int) { exitCode = code })
	l.D("debug")
	eq("[D] debug\n")
	l.I("info")
	eq("[I] info\n")
	l.W("warning")
	eq("[W] warning\n")
	l.E("error")
	eq("[E] error\n")
	l.Msg(&LogMsg{Level: 'X', Msg: "msg"})
	eq("[X] msg\n")
	defer func() {
		eq("[F] fatal\n")
		assert.NotNil(t, recover())
		assert.Equal(t, 1, exitCode)
	}()
	l.F("fatal")
	panic(nil)
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
		buf.WriteByte('\n')
	})
	Log.I("func")
	assert.Equal(t, "I func\n", buf.String())
	buf.Reset()
	Log.SetFunc(nil)
	Log.I("nil")
	assert.Empty(t, buf.String())
}
