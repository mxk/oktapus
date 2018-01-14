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
	match, err := ctx.Accounts(args[1])
	if err != nil {
		return err
	}

	// Select available accounts at random
	match.Shuffle()
	alloc := make(Accounts, 0, n)
	for _, ac := range match {
		if ac.Err == nil && ac.Owner == "" {
			if _, err := ac.Creds().Get(); err == nil {
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
	owner := ctx.AWS().CommonRole
	if cmd.owner != "" {
		owner = cmd.owner
	}
	for _, ac := range alloc {
		ac.Owner = owner
	}
	return cmd.PrintOutput(listCreds(alloc.Save(false)))
}
