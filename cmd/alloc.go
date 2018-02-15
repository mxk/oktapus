package cmd

import (
	"bufio"
	"flag"
	"fmt"
	"strconv"
	"time"

	"github.com/LuminalHQ/oktapus/op"
)

func init() {
	op.Register(&op.CmdInfo{
		Names:   []string{"alloc"},
		Summary: "Allocate accounts",
		Usage:   "[options] [num] [account-spec]",
		MinArgs: 1,
		MaxArgs: 2,
		New:     func() op.Cmd { return &alloc{Name: "alloc"} },
	})
}

type alloc struct {
	Name
	PrintFmt
	Owner string
	Num   int
	Spec  string
}

func (cmd *alloc) Help(w *bufio.Writer) {
	op.WriteHelp(w, `
		Account allocation assigns an owner to an account, preventing anyone
		else from allocating that account until it is freed. The owner is
		effectively a per-account mutex.

		You can specify the number of accounts to allocate along with account
		filtering specifications. One or the other may be omitted, but not both.
		If the number is not specified, all matching accounts are allocated.
		Otherwise, that number of requested accounts are allocated randomly from
		the pool of all matching accounts.
	`)
	op.AccountSpecHelp(w)
}

func (cmd *alloc) FlagCfg(fs *flag.FlagSet) {
	cmd.PrintFmt.FlagCfg(fs)
	fs.StringVar(&cmd.Owner, "owner", "", "Override default owner `name`")
}

func (cmd *alloc) Run(ctx *op.Ctx, args []string) error {
	n, err := strconv.Atoi(args[0])
	if err == nil {
		if n < 1 || 100 < n {
			op.UsageErr(cmd, "number of accounts must be between 1 and 100")
		}
		padArgs(cmd, &args)
		args = args[1:]
	} else if len(args) != 1 {
		op.UsageErr(cmd, "first argument must be a number")
	} else {
		n = -1
	}
	cmd.Num = n
	cmd.Spec = args[0]
	out, err := ctx.Call(cmd)
	if err == nil {
		err = cmd.Print(out)
	}
	return err
}

func (cmd *alloc) Call(ctx *op.Ctx) (interface{}, error) {
	// Find free accounts and randomize their order
	acs, err := ctx.Accounts(cmd.Spec)
	if err != nil {
		return nil, err
	}
	acs = acs.Filter(func(ac *op.Account) bool {
		return ac.Err == nil && ac.Owner == ""
	}).Shuffle()
	n := cmd.Num
	if n == -1 {
		n = len(acs)
	}

	// Allocate in batches
	owner := ctx.AWS().CommonRole.Name()
	if cmd.Owner != "" {
		owner = cmd.Owner
	}
	alloc := make(op.Accounts, 0, n)
	for n > 0 {
		if len(acs) < n {
			// Not enough accounts, free any that were already allocated
			for _, ac := range alloc {
				ac.Owner = ""
			}
			alloc.Save()
			n -= len(acs)
			s := "s"
			if n == 1 {
				s = ""
			}
			return nil, fmt.Errorf("allocation failed (need %d more account%s)",
				n, s)
		}

		// Set owner
		batch := acs[:n]
		acs = acs[n:]
		for _, ac := range batch {
			ac.Owner = owner
		}
		batch.Save().Filter(func(ac *op.Account) bool {
			return ac.Err == nil
		})

		// Delay determined by running 1,100 mutex-test trials with 50 threads
		time.Sleep(10 * time.Second)

		// Verify owner
		batch.RefreshCtl().Filter(func(ac *op.Account) bool {
			return ac.Err == nil && ac.Owner == owner
		})
		n -= len(batch)
		alloc = append(alloc, batch...)
	}
	return listCreds(alloc.Sort(), false), nil
}
