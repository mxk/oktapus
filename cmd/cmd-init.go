package cmd

import (
	"bufio"
	"errors"
)

func init() {
	register(&cmdInfo{
		names:   []string{"init"},
		summary: "Initialize account control information",
		usage:   "[options] account-spec",
		minArgs: 1,
		maxArgs: 1,
		new:     func() Cmd { return &initCmd{Name: "init"} },
	})
}

type initCmd struct {
	Name
	PrintFmt
	Spec string
}

func (cmd *initCmd) Help(w *bufio.Writer) {
	writeHelp(w, `
		Initialize account control information.

		The account owner, description, and tags are stored within the account
		itself, encoded in the description of a well-known IAM role. Accounts
		that do not have this role are not managed by oktapus. This command
		creates the role and initializes the account control structure.
	`)
	accountSpecHelp(w)
}

func (cmd *initCmd) Run(ctx *Ctx, args []string) error {
	cmd.Spec = args[0]
	out, err := ctx.Call(cmd)
	if err == nil {
		err = cmd.Print(out)
	}
	return err
}

func (cmd *initCmd) Call(ctx *Ctx) (interface{}, error) {
	acs, err := ctx.Accounts(cmd.Spec)
	if err != nil {
		return nil, err
	}
	errInit := errors.New("already initialized")
	acs.Apply(func(ac *Account) {
		if ac.Ctl == nil {
			ac.Ctl = new(Ctl)
			ac.Err = ac.Ctl.init(ac.IAM)
			// TODO: Use errInit if role exists
		} else if ac.Err == nil {
			ac.Err = errInit
		}
	})
	return listResults(acs), nil
}
