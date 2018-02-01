package cmd

import (
	"bufio"
	"flag"
)

func init() {
	register(&cmdInfo{
		names:   []string{"free"},
		summary: "Release accounts",
		usage:   "[options] [account-spec]",
		minArgs: 0,
		maxArgs: 1,
		new:     func() Cmd { return &free{Name: "free"} },
	})
}

type free struct {
	Name
	PrintFmt
	Force bool
	Spec  string
}

func (cmd *free) Help(w *bufio.Writer) {
	writeHelp(w, `
		Release owned accounts.

		Freeing an account allows someone else to allocate it. If the account
		contains any temporary IAM users or roles, those are deleted (see authz
		and creds commands for more info).
	`)
	accountSpecHelp(w)
}

func (cmd *free) FlagCfg(fs *flag.FlagSet) {
	cmd.PrintFmt.FlagCfg(fs)
	fs.BoolVar(&cmd.Force, "force", false,
		"Release account even if you are not the owner")
}

func (cmd *free) Run(ctx *Ctx, args []string) error {
	padArgs(cmd, &args)
	cmd.Spec = args[0]
	out, err := ctx.Call(cmd)
	if err == nil {
		err = cmd.Print(out)
	}
	return err
}

func (cmd *free) Call(ctx *Ctx) (interface{}, error) {
	acs, err := ctx.Accounts(cmd.Spec)
	if err != nil {
		return nil, err
	}
	commonRole := ctx.AWS().CommonRole
	acs = acs.Filter(func(ac *Account) bool {
		return ac.Err == nil && (cmd.Force || ac.Owner == commonRole)
	})

	// Clear owner and delete temporary users/roles
	acs.Apply(func(ac *Account) {
		ac.Owner = ""
		ch := make(chan error, 1)
		go func() { ch <- delTmpRoles(ac.IAM) }()
		ac.Err = delTmpUsers(ac.IAM)
		if err := <-ch; ac.Err == nil {
			ac.Err = err
		}
	})

	// Save owner changes only if all temporary users/roles were deleted
	tmp := append(make(Accounts, 0, len(acs)), acs...)
	tmp.Filter(func(ac *Account) bool {
		return ac.Err == nil
	}).Save()
	return listResults(acs), nil
}
