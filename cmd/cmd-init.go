package cmd

import (
	"bufio"
	"errors"
)

func init() {
	register(&Init{command: command{
		name:    []string{"init"},
		summary: "Initialize account control information",
		usage:   "[options] account-spec",
		minArgs: 1,
		maxArgs: 1,
	}})
}

type Init struct{ command }

func (cmd *Init) Help(w *bufio.Writer) {
	writeHelp(w, `
		Initialize account control information.

		The account owner, description, and tags are stored within the account
		itself, encoded in the description of a well-known IAM role. Accounts
		that do not have this role are not managed by oktapus. This command
		creates the role and initializes the account control structure.
	`)
	accountSpecHelp(w)
}

func (cmd *Init) Run(ctx *Ctx, args []string) error {
	match, err := ctx.Accounts(args[0])
	if err != nil {
		return err
	}
	errInit := errors.New("already initialized")
	match.Apply(func(ac *Account) {
		if ac.Ctl == nil {
			ac.Ctl = new(Ctl)
			ac.Err = ac.Ctl.init(ac.IAM)
			// TODO: Use errInit if role exists
		} else if ac.Err == nil {
			ac.Err = errInit
		}
	})
	return cmd.PrintOutput(listResults(match))
}
