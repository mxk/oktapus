package cmd

import (
	"flag"
	"fmt"
	"sort"
	"strconv"
)

func init() {
	register(&Alloc{command: command{
		name:    []string{"alloc"},
		summary: "Allocate accounts",
		usage:   "[options] num [account-spec]",
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
	fs.StringVar(&cmd.owner, "owner", "", "Override default owner `name`")
}

func (cmd *Alloc) Run(ctx *Ctx, args []string) error {
	cmd.PadArgs(&args)
	n, err := strconv.Atoi(args[0])
	if err != nil {
		usageErr(cmd, "first argument must be a number")
	} else if n <= 0 {
		usageErr(cmd, "number of accounts must be > 0")
	}
	c := ctx.AWS()
	match, err := getAccounts(c, args[1])
	if err != nil {
		return err
	}

	// Select available accounts at random
	shuffle(match)
	alloc := make([]*Account, 0, n)
	for _, ac := range match {
		if ctl := ac.Ctl(); ac.Error() == nil && ctl.Owner == "" {
			if _, err := c.Creds(ac.ID).Get(); err == nil {
				if alloc = append(alloc, ac); len(alloc) == n {
					break
				}
			}
		}
	}
	if len(alloc) < n {
		return fmt.Errorf("insufficient accounts (have=%d, want=%d)",
			len(alloc), n)
	}
	sort.Sort(byName(alloc))

	// Allocate selected accounts
	owner := c.CommonRole
	if cmd.owner != "" {
		owner = cmd.owner
	}
	out := make([]*CredsOutput, 0, len(alloc))
	for _, ac := range alloc {
		cr := newCredsOutput(ac, c.Creds(ac.ID))
		if cr.Error == "" {
			ac.Ctl().Owner = owner
			cr.Error = explainError(ac.Save())
		}
		out = append(out, cr)
	}
	return cmd.PrintOutput(out)
}
