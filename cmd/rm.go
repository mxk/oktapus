package cmd

import (
	"encoding/gob"
	"fmt"
	"sort"

	"github.com/LuminalHQ/oktapus/internal"
	"github.com/LuminalHQ/oktapus/op"
	"github.com/aws/aws-sdk-go/service/iam/iamiface"
)

func init() {
	op.Register(&op.CmdInfo{
		Names:   []string{"rm"},
		Summary: "Remove IAM users/roles",
		Usage:   "account-spec {role|user} name [name ...]",
		MinArgs: 3,
		New:     func() op.Cmd { return &rm{Name: "rm"} },
	})
	gob.Register([]*rmOutput{})
}

type rm struct {
	Name
	PrintFmt
	Spec  string
	Type  string
	Names []string
}

func (cmd *rm) Run(ctx *op.Ctx, args []string) error {
	cmd.Spec, cmd.Type, cmd.Names = args[0], args[1], args[2:]
	switch cmd.Type {
	case "role", "user":
	default:
		op.UsageErr(cmd, "invalid resource type %q", cmd.Type)
	}
	out, err := ctx.Call(cmd)
	if err == nil {
		err = cmd.Print(out)
	}
	return err
}

func (cmd *rm) Call(ctx *op.Ctx) (interface{}, error) {
	var fn func(c iamiface.IAMAPI, name string) error
	switch cmd.Type {
	case "role":
		fn = op.DelRole
	case "user":
		fn = op.DelUser
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
		if _, err := ac.Creds(false); err != nil {
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
	err = internal.GoForEach(len(res), 0, func(i int) error {
		ac, name, r := acs[i/len(names)], names[i%len(names)], "OK"
		if err := fn(ac.IAM(), name); err != nil {
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
