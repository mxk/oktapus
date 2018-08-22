package cmd

import (
	"math/rand"
	"strconv"
	"time"

	"github.com/LuminalHQ/cloudcover/oktapus/op"
	"github.com/LuminalHQ/cloudcover/x/cli"
	"github.com/LuminalHQ/cloudcover/x/fast"
	"github.com/pkg/errors"
)

var allocCli = cli.Main.Add(&cli.Info{
	Name:    "alloc",
	Usage:   "[options] [num] [account-spec]",
	Summary: "Allocate accounts for exclusive use",
	MinArgs: 1,
	MaxArgs: 2,
	New:     func() cli.Cmd { return &allocCmd{} },
})

type allocCmd struct {
	OutFmt
	Owner string `flag:"Set owner <name>"`
	Num   int
	Spec  string
}

func (*allocCmd) Info() *cli.Info { return allocCli }

func (*allocCmd) Help(w *cli.Writer) {
	w.Text(`
	Allocate accounts for exclusive use.

	Account allocation assigns an owner to an account, preventing anyone else
	from allocating that account until it is freed.

	Allocation is an advisory concept that only exists within oktapus. It is not
	enforced in any way by AWS. Anyone with valid credentials may still access
	an allocated account through the CLI or the Management Console, but they
	will not be able to allocate it with oktapus.

	You can specify the number of accounts to allocate along with the account
	spec. One or the other may be omitted, but not both. If the number is not
	specified, all free matching accounts are allocated. Otherwise, the
	requested number of random accounts are allocated from the match pool.
	`)
	accountSpecHelp(w)
}

func (cmd *allocCmd) Main(args []string) error {
	n, err := strconv.Atoi(args[0])
	if err == nil {
		if n < 1 || 100 < n {
			return cli.Error("number of accounts must be between 1 and 100")
		}
		args = args[1:]
	} else if len(args) != 1 {
		return cli.Error("first argument must be a number")
	}
	cmd.Num = n
	cmd.Spec = get(args, 0)
	return op.RunAndPrint(cmd)
}

func (cmd *allocCmd) Run(ctx *op.Ctx) (interface{}, error) {
	// Find free accounts and randomize their order
	acs, err := ctx.Match(cmd.Spec)
	if err != nil {
		return nil, err
	}
	acs = acs.Filter(func(ac *op.Account) bool {
		return ac.CtlValid() && ac.Ctl.Owner == "" && ac.Err == nil
	})
	rand.Shuffle(len(acs), func(i, j int) { acs[i], acs[j] = acs[j], acs[i] })

	// Allocate in batches
	if cmd.Owner == "" {
		cmd.Owner = ctx.Role().Name()
	}
	if cmd.Num == 0 {
		cmd.Num = len(acs)
	}
	out := make(op.Accounts, 0, cmd.Num)
	for n := cmd.Num; n > 0; {
		if len(acs) < n {
			// Not enough accounts, free any that were already allocated
			out.Filter(func(ac *op.Account) bool {
				if ac.Err != nil {
					return false
				}
				ac.Ctl.Owner = ""
				return true
			}).StoreCtl()
			n -= len(acs)
			return nil, errors.Errorf("not enough accounts, need %d more", n)
		}

		// Set owner
		batch := acs[:n]
		acs = acs[n:]
		for _, ac := range batch {
			ac.Ctl.Owner = cmd.Owner
		}
		batch.StoreCtl()

		// Verify owner after a delay to allow changes to propagate. Delay was
		// selected by running 1,100 mutex-test trials with 50 threads without
		// seeing any inconsistencies.
		fast.Sleep(10 * time.Second)
		for _, ac := range batch.LoadCtl(true) {
			if ac.Err == nil {
				if ac.Ctl.Owner == cmd.Owner {
					n--
				} else {
					ac.Err = op.ErrCtlUpdate
				}
			}
		}
		out = append(out, batch...)
	}
	return listOwners(out.SortByName()), nil
}

type ownerOutput struct {
	Account string
	Name    string
	Owner   string
	Result  string
}

func listOwners(acs op.Accounts) []*ownerOutput {
	out := make([]*ownerOutput, len(acs))
	for i, ac := range acs {
		result := "OK"
		if ac.Err != nil {
			result = "ERROR: " + explainError(ac.Err)
		}
		out[i] = &ownerOutput{
			Account: ac.ID,
			Name:    ac.Name,
			Owner:   ac.Ctl.Owner,
			Result:  result,
		}
	}
	return out
}
