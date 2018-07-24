package cmd

import (
	"fmt"
	"math/rand"
	"strconv"
	"time"

	"github.com/LuminalHQ/cloudcover/oktapus/internal"
	"github.com/LuminalHQ/cloudcover/oktapus/op"
	"github.com/LuminalHQ/cloudcover/x/cli"
)

var allocCli = register(&cli.Info{
	Name:    "alloc",
	Usage:   "[options] [num] [account-spec]",
	Summary: "Allocate accounts",
	MinArgs: 1,
	MaxArgs: 2,
	New:     func() cli.Cmd { return &allocCmd{} },
})

type allocCmd struct {
	OutFmt
	Owner string `flag:"Override default owner <name>"`
	Num   int
	Spec  string
}

func (cmd *allocCmd) Info() *cli.Info { return allocCli }

func (cmd *allocCmd) Help(w *cli.Writer) {
	w.Text(`
	Account allocation assigns an owner to an account, preventing anyone else
	from allocating that account until it is freed. The owner is effectively a
	per-account mutex.

	You can specify the number of accounts to allocate along with account
	filtering specifications. One or the other may be omitted, but not both. If
	the number is not specified, all matching accounts are allocated. Otherwise,
	that number of requested accounts are allocated randomly from the pool of
	all matching accounts.
	`)
	accountSpecHelp(w)
}

func (cmd *allocCmd) Main(args []string) error {
	return cmd.Run(op.NewCtx(), args)
}

func (cmd *allocCmd) Run(ctx *op.Ctx, args []string) error {
	n, err := strconv.Atoi(args[0])
	if err == nil {
		if n < 1 || 100 < n {
			return cli.Error("number of accounts must be between 1 and 100")
		}
		padArgs(cmd, &args)
		args = args[1:]
	} else if len(args) != 1 {
		return cli.Error("first argument must be a number")
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

func (cmd *allocCmd) Call(ctx *op.Ctx) (interface{}, error) {
	// Find free accounts and randomize their order
	acs, err := ctx.Accounts(cmd.Spec)
	if err != nil {
		return nil, err
	}
	acs = acs.Filter(func(ac *op.Account) bool {
		return ac.Err == nil && ac.Owner == ""
	})
	n := cmd.Num
	if n == -1 {
		n = len(acs)
	}
	rand.Shuffle(len(acs), func(i, j int) { acs[i], acs[j] = acs[j], acs[i] })

	// Allocate in batches
	owner := ctx.Gateway().CommonRole.Name()
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
		internal.Sleep(10 * time.Second)

		// Verify owner
		batch.RefreshCtl().Filter(func(ac *op.Account) bool {
			return ac.Err == nil && ac.Owner == owner
		})
		n -= len(batch)
		alloc = append(alloc, batch...)
	}
	return listCreds(alloc.Sort(), false), nil
}
