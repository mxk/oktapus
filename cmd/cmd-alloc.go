package cmd

import (
	"flag"
	"fmt"
	"strconv"
)

func init() {
	register(&Alloc{command: command{
		name:    []string{"alloc"},
		summary: "Allocate accounts",
		usage:   "[options] count [account-spec]",
		minArgs: 1,
		maxArgs: 2,
	}})
}

type Alloc struct {
	command
	owner string
}

func (cmd *Alloc) FlagCfg(fs *flag.FlagSet) {
	cmd.command.FlagCfg(fs)
	fs.StringVar(&cmd.owner, "owner", "", "Set explicit owner `id`")
}

func (cmd *Alloc) Run(ctx *Ctx, args []string) error {
	cmd.PadArgs(&args)
	n, err := strconv.Atoi(args[0])
	if n <= 0 || err != nil {
		return err
	}
	c := ctx.AWS()
	match, err := getAccounts(c, args[1])
	if err != nil {
		return err
	}
	shuffle(match)
	alloc := make([]*Account, 0, n)
	for _, ac := range match {
		if ctl := ac.Ctl(); ac.Error() == nil && ctl.Owner == "" {
			if alloc = append(alloc, ac); len(alloc) == n {
				break
			}
		}
	}
	if len(alloc) < n {
		return fmt.Errorf("insufficient accounts (available=%d, want=%d)",
			len(alloc), n)
	}
	creds := cmds["creds"].(*Creds)
	out := creds.Get(c, alloc)
	setOwner := true
	for _, c := range out {
		if c.Error != "" {
			setOwner = false
			break
		}
	}
	if setOwner {
		owner := c.CommonRole
		if cmd.owner != "" {
			owner = cmd.owner
		}
		for i, ac := range alloc {
			ac.Ctl().Owner = owner
			out[i].Error = explainError(ac.Save())
		}
	}
	return cmd.PrintOutput(creds.out(out))
}
