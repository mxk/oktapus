package cmd

import (
	"flag"
	"fmt"
	"strconv"
	"time"
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

	// Find free accounts and randomize their order
	acs, err := ctx.Accounts(args[1])
	if err != nil {
		return err
	}
	acs = acs.Filter(func(ac *Account) bool {
		return ac.Err == nil && ac.Owner == ""
	}).Shuffle()

	// Allocate in batches
	owner := ctx.AWS().CommonRole
	if cmd.owner != "" {
		owner = cmd.owner
	}
	alloc := make(Accounts, 0, n)
	for n > 0 {
		if len(acs) < n {
			// Not enough accounts, free any that were already allocated
			for _, ac := range alloc {
				ac.Owner = ""
			}
			alloc.Save()
			return fmt.Errorf("allocation failed (need %d more accounts)",
				n-len(acs))
		}

		// Set owner
		batch := acs[:n]
		acs = acs[n:]
		for _, ac := range batch {
			ac.Owner = owner
		}
		batch.Save().Filter(func(ac *Account) bool {
			return ac.Err == nil
		})

		// TODO: Adjust delay
		time.Sleep(1 * time.Second)

		// Verify owner
		batch.RefreshCtl().Filter(func(ac *Account) bool {
			return ac.Err == nil && ac.Owner == owner
		})
		n -= len(batch)
		alloc = append(alloc, batch...)
	}
	return cmd.PrintOutput(listCreds(alloc.Sort()))
}
