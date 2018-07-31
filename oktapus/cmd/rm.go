package cmd

import (
	"fmt"
	"sort"

	"github.com/LuminalHQ/cloudcover/oktapus/awsx"
	"github.com/LuminalHQ/cloudcover/oktapus/op"
	"github.com/LuminalHQ/cloudcover/x/cli"
	"github.com/LuminalHQ/cloudcover/x/fast"
	"github.com/aws/aws-sdk-go-v2/service/iam"
)

var rmCli = register(&cli.Info{
	Name:    "rm",
	Usage:   "account-spec {role|user} name [name ...]",
	Summary: "Remove IAM users/roles",
	MinArgs: 3,
	New:     func() cli.Cmd { return &rmCmd{} },
})

type rmCmd struct {
	OutFmt
	Spec  string
	Type  string
	Names []string
}

func (cmd *rmCmd) Info() *cli.Info { return rmCli }

func (cmd *rmCmd) Help(w *cli.Writer) {
	w.Text("Remove IAM users/roles.")
	accountSpecHelp(w)
}

func (cmd *rmCmd) Main(args []string) error {
	return cmd.Run(op.NewCtx(), args)
}

func (cmd *rmCmd) Run(ctx *op.Ctx, args []string) error {
	cmd.Spec, cmd.Type, cmd.Names = args[0], args[1], args[2:]
	switch cmd.Type {
	case "role", "user":
	default:
		return cli.Errorf("invalid resource type %q", cmd.Type)
	}
	out, err := ctx.Call(cmd)
	if err == nil {
		err = cmd.Print(out)
	}
	return err
}

func (cmd *rmCmd) Call(ctx *op.Ctx) (interface{}, error) {
	var fn func(c iam.IAM, name string) error
	switch cmd.Type {
	case "role":
		fn = awsx.DeleteRole
	case "user":
		fn = awsx.DeleteUser
	default:
		return nil, fmt.Errorf("invalid resource type %q", cmd.Type)
	}
	acs, err := ctx.Accounts(cmd.Spec)
	if err != nil || len(acs) == 0 {
		return nil, err
	}

	// Filter out inaccessible accounts
	out := make([]*rmOutput, 0, len(acs)*len(cmd.Names))
	acs = acs.Filter(func(ac *op.Account) bool {
		if _, err := ac.CredsProvider().Creds(); err != nil {
			out = append(out, &rmOutput{
				AccountID: ac.ID,
				Name:      ac.Name,
				Result:    "ERROR: " + explainError(err),
			})
			return false
		}
		return true
	})
	valid := len(acs) * len(cmd.Names)
	if valid == 0 {
		return out, err
	}
	out = out[:len(out)+valid]
	res := out[len(out)-valid:]

	// Remove resources
	names := cmd.Names
	err = fast.ForEachIO(len(res), func(i int) error {
		ac, name, r := acs[i/len(names)], names[i%len(names)], "OK"
		if err := fn(*ac.IAM(), name); err != nil {
			r = "ERROR: " + explainError(err)
		}
		res[i] = &rmOutput{
			AccountID: ac.ID,
			Name:      ac.Name,
			Resource:  name,
			Result:    r,
		}
		return nil
	})
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		return a.Name < b.Name || (a.Name == b.Name && a.Resource < b.Resource)
	})
	return out, err
}

type rmOutput struct{ AccountID, Name, Resource, Result string }
