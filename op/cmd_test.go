package op

import (
	"bufio"
	"flag"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestStringPtrVar(t *testing.T) {
	ptr := func(s string) *string { return &s }
	tests := []struct {
		args []string
		want *string
	}{
		{[]string{}, nil},
		{[]string{"-s="}, ptr("")},
		{[]string{"-s=x"}, ptr("x")},
		{[]string{"-s=x", "-s=yz"}, ptr("yz")},
	}
	for _, test := range tests {
		fs := flag.NewFlagSet("", flag.PanicOnError)
		var s *string
		StringPtrVar(fs, &s, "s", "")
		fs.Parse(test.args)
		assert.Equal(t, test.want, s)
	}
}

func TestRun(t *testing.T) {
	cmd := new(testCmd)
	Register(cmd.Info())
	Run([]string{"testcmd", "-flag=1", "arg1", "arg2"})
	assert.True(t, cmd.flag)
	assert.Equal(t, []string{"arg1", "arg2"}, cmd.args)
}

type testCmd struct {
	flag bool
	args []string
}

func (cmd *testCmd) Info() *CmdInfo {
	return &CmdInfo{
		Names:   []string{"testcmd"},
		MinArgs: 1,
		MaxArgs: 2,
		New:     func() Cmd { return cmd },
	}
}

func (testCmd) Help(w *bufio.Writer) {}

func (cmd *testCmd) FlagCfg(fs *flag.FlagSet) {
	fs.BoolVar(&cmd.flag, "flag", false, "")
}

func (cmd *testCmd) Run(ctx *Ctx, args []string) error {
	cmd.args = args
	return nil
}
