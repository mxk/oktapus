package cmd

import (
	"bufio"
	"bytes"
	"flag"
	"io/ioutil"

	"github.com/LuminalHQ/oktapus/mock"
	"github.com/LuminalHQ/oktapus/op"
)

// newCmd creates a new command instance and parses flag arguments.
func newCmd(name string, args ...string) op.Cmd {
	cmd := op.GetCmdInfo(name).New()
	fs := flag.FlagSet{Usage: func() {}}
	fs.SetOutput(ioutil.Discard)
	cmd.FlagCfg(&fs)
	if err := fs.Parse(args); err != nil {
		panic(err)
	}
	var buf bytes.Buffer
	bio := bufio.NewWriter(&buf)
	cmd.Help(bio)
	bio.Flush()
	if buf.Len() == 0 {
		panic("command " + name + " does not provide help information")
	}
	return cmd
}

// initCtl initializes account control information for unit tests.
func initCtl(ctx *op.Ctx, ctl *op.Ctl, ids ...string) error {
	var empty op.Ctl
	if ctl == nil {
		ctl = &empty
	}
	c := ctx.AWS()
	for _, id := range ids {
		id = mock.AccountID(id)
		ac := op.NewAccount(id, "")
		ac.Init(c.ConfigProvider(), c.CredsProvider(id))
		if err := ctl.Init(ac.IAM()); err != nil {
			return err
		}
	}
	return nil
}
