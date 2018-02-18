package op

import (
	"bufio"
	"bytes"
	"flag"
	"testing"

	"github.com/LuminalHQ/oktapus/internal"
	"github.com/stretchr/testify/assert"
)

func TestHelp(t *testing.T) {
	assert.True(t, isHelp("help"))

	var buf bytes.Buffer
	code := -1
	helpWriter = &buf
	helpExitFunc = func(c int) {
		if code == -1 {
			code = c
		}
	}
	cmd := Cmd(helpCmd{})
	ci := cmd.Info()
	Register(ci)

	// General help
	Help(nil)
	out := buf.String()
	assert.Equal(t, 0, code)
	assert.Contains(t, out, ci.Names[0])
	assert.Contains(t, out, ci.Summary)
	code = -1
	buf.Reset()

	// Command help
	Help(ci)
	out = buf.String()
	assert.Equal(t, 0, code)
	assert.Contains(t, out, ci.Usage)
	assert.Contains(t, out, "Command help")
	assert.Contains(t, out, "-testflag")
	code = -1
	buf.Reset()

	// General error
	UsageErrf(nil, "general error")
	want := internal.Dedent(`
		Error: general error
		Usage: oktapus command [options] args
		       oktapus command help
		       oktapus help [command]
	`)
	assert.Equal(t, 2, code)
	assert.Equal(t, want[1:], buf.String())
	code = -1
	buf.Reset()

	// Command error
	UsageErrf(cmd, "command error")
	want = internal.Dedent(`
		Error: command error
		Usage: oktapus {helpcmd|helpalias} [-testflag]
		       oktapus {helpcmd|helpalias} help
	`)
	assert.Equal(t, 2, code)
	assert.Equal(t, want[1:], buf.String())
	code = -1
	buf.Reset()

	// Panic reported before calling helpExitFunc
	cmd = panicCmd{}
	ci = cmd.Info()
	Register(ci)
	Help(ci)
	assert.Equal(t, 2, code)
	assert.Contains(t, buf.String(), "panic: help panic")
}

type helpCmd struct{}

func (helpCmd) Info() *CmdInfo {
	return &CmdInfo{
		Names:   []string{"helpcmd", "helpalias"},
		Summary: "test command summary",
		Usage:   "[-testflag]",
		New:     func() Cmd { return helpCmd{} },
	}
}

func (helpCmd) Help(w *bufio.Writer)              { w.WriteString("Command help\n") }
func (helpCmd) FlagCfg(fs *flag.FlagSet)          { fs.String("testflag", "", "") }
func (helpCmd) Run(ctx *Ctx, args []string) error { return nil }

type panicCmd struct{ helpCmd }

func (panicCmd) Info() *CmdInfo {
	return &CmdInfo{
		Names: []string{"paniccmd"},
		New:   func() Cmd { return panicCmd{} },
	}
}

func (panicCmd) FlagCfg(fs *flag.FlagSet) { panic("help panic") }
