package op

import (
	"bufio"
	"bytes"
	"flag"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestHelp(t *testing.T) {
	assert.True(t, isHelp("help"))

	var buf bytes.Buffer
	var code int
	helpWriter = &buf
	helpExitFunc = func(c int) { code = c }
	cmd := Cmd(helpCmd{})
	ci := cmd.Info()
	Register(ci)

	err := "general error"
	UsageErr(nil, err)
	assert.Equal(t, 2, code)
	help := buf.String()
	assert.Contains(t, help, err)
	assert.Contains(t, help, ci.Names[0])
	assert.Contains(t, help, ci.Summary)

	code = 0
	buf.Reset()

	err = "command error"
	UsageErr(cmd, err)
	assert.Equal(t, 2, code)
	help = buf.String()
	assert.Contains(t, help, err)
	for _, name := range ci.Names {
		assert.Contains(t, help, name)
	}
	assert.Contains(t, help, "Command help")
	assert.Contains(t, help, "testflag")

	code = 0
	buf.Reset()

	cmd = panicCmd{}
	ci = cmd.Info()
	Register(ci)
	CmdHelp(ci, nil)
	help = buf.String()
	assert.Contains(t, help, "panic: help panic")
}

type helpCmd struct{}

func (helpCmd) Info() *CmdInfo {
	return &CmdInfo{
		Names:   []string{"testcmd", "testalias"},
		Summary: "test command summary",
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
