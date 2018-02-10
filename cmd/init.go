package cmd

import (
	"bufio"
	"errors"

	"github.com/LuminalHQ/oktapus/op"
)

func init() {
	op.Register(&op.CmdInfo{
		Names:   []string{"init"},
		Summary: "Initialize account control information",
		Usage:   "[options] account-spec",
		MinArgs: 1,
		MaxArgs: 1,
		New:     func() op.Cmd { return &initCmd{Name: "init"} },
	})
}

type initCmd struct {
	Name
	PrintFmt
	Spec string
}

func (cmd *initCmd) Help(w *bufio.Writer) {
	op.WriteHelp(w, `
		Initialize account control information.

		The account owner, description, and tags are stored within the account
		itself, encoded in the description of a well-known IAM role. Accounts
		that do not have this role are not managed by oktapus. This command
		creates the role and initializes the account control structure.
	`)
	op.AccountSpecHelp(w)
}

func (cmd *initCmd) Run(ctx *op.Ctx, args []string) error {
	cmd.Spec = args[0]
	out, err := ctx.Call(cmd)
	if err == nil {
		err = cmd.Print(out)
	}
	return err
}

func (cmd *initCmd) Call(ctx *op.Ctx) (interface{}, error) {
	acs, err := ctx.Accounts(cmd.Spec)
	if err != nil {
		return nil, err
	}
	errInit := errors.New("already initialized")
	acs.Apply(func(_ int, ac *op.Account) {
		if ac.Ctl == nil {
			ac.Ctl = &op.Ctl{Tags: []string{"init"}}
			ac.Err = ac.Ctl.Init(ac.IAM())
			// TODO: Use errInit if role exists
		} else if ac.Err == nil {
			ac.Err = errInit
		}
	})
	return listResults(acs), nil
}
