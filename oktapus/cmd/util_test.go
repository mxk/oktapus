package cmd

import (
	"bufio"
	"bytes"
	"flag"
	"io/ioutil"
	"testing"

	"github.com/LuminalHQ/cloudcover/oktapus/mock"
	"github.com/LuminalHQ/cloudcover/oktapus/op"
	"github.com/stretchr/testify/require"
)

func TestHelp(t *testing.T) {
	var buf bytes.Buffer
	bio := bufio.NewWriter(&buf)
	for _, name := range op.CmdNames() {
		op.GetCmdInfo(name).New().Help(bio)
		bio.Flush()
		require.NotZero(t, buf.Len(), "command=%s", name)
		buf.Reset()
	}
}

// newCtx returns a Ctx for testing commands, optionally initializing account
// control information for the specified account IDs.
func newCtx(init ...string) *op.Ctx {
	ctx := &op.Ctx{Sess: mock.NewSession()}
	if len(init) > 0 {
		if err := initCtl(ctx, nil, init...); err != nil {
			panic(err)
		}
	}
	return ctx
}

// newCmd creates a new command instance and parses flag arguments.
func newCmd(name string, args ...string) op.Cmd {
	cmd := op.GetCmdInfo(name).New()
	fs := flag.FlagSet{Usage: func() {}}
	fs.SetOutput(ioutil.Discard)
	cmd.FlagCfg(&fs)
	if err := fs.Parse(args); err != nil {
		panic(err)
	}
	return cmd
}

// initCtl initializes account control information for unit tests.
func initCtl(ctx *op.Ctx, ctl *op.Ctl, ids ...string) error {
	var empty op.Ctl
	if ctl == nil {
		ctl = &empty
	}
	gw := ctx.Gateway()
	for _, id := range ids {
		id = mock.AccountID(id)
		ac := op.NewAccount(id, "")
		ac.Init(gw, gw.CredsProvider(id))
		if err := ctl.Init(ac.IAM()); err != nil {
			return err
		}
	}
	return nil
}
