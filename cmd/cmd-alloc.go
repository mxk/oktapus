package cmd

import (
	"bufio"
	"flag"
	"fmt"
	"strconv"
	"time"
)

func init() {
	register(&Alloc{command: command{
		name:    []string{"alloc"},
		summary: "Allocate accounts",
		usage:   "[options] [num] [account-spec]",
		minArgs: 1,
		maxArgs: 2,
	}})
}

type Alloc struct {
	command
	owner string
}

func (cmd *Alloc) Help(w *bufio.Writer) {
	writeHelp(w, `
		Account allocation assigns an owner to an account, preventing anyone
		else from allocating that account until it is freed. The owner is
		effectively a per-account mutex.

		You can specify the number of accounts to allocate along with account
		filtering specifications. One or the other may be omitted, but not both.
		If the number is not specified, all matching accounts are allocated.
		Otherwise, that number of requested accounts are allocated randomly from
		the pool of all matching accounts.
	`)
	accountSpecHelp(w)
}

func (cmd *Alloc) FlagCfg(fs *flag.FlagSet) {
	cmd.command.FlagCfg(fs)
	fs.StringVar(&cmd.owner, "owner", "", "Override default owner `name`")
}

func (cmd *Alloc) Run(ctx *Ctx, args []string) error {
	n, err := strconv.Atoi(args[0])
	if err == nil {
		if n < 1 || 100 < n {
			usageErr(cmd, "number of accounts must be between 1 and 100")
		}
		cmd.PadArgs(&args)
		args = args[1:]
	} else if len(args) != 1 {
		usageErr(cmd, "first argument must be a number")
	} else {
		n = -1
	}

	// Find free accounts and randomize their order
	acs, err := ctx.Accounts(args[0])
	if err != nil {
		return err
	}
	acs = acs.Filter(func(ac *Account) bool {
		return ac.Err == nil && ac.Owner == ""
	}).Shuffle()
	if n == -1 {
		n = len(acs)
	}

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
			return fmt.Errorf("allocation failed (need %d more account(s))",
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
	return cmd.PrintOutput(listCreds(alloc.Sort(), false))
}
