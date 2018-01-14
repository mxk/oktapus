package cmd

import (
	"strings"

	"github.com/LuminalHQ/oktapus/awsgw"
)

func init() {
	register(&Create{command: command{
		name:    []string{"create"},
		summary: "Create new accounts",
		usage:   "[options] name:email [name:email ...]",
		minArgs: 1,
	}})
}

// TODO: Restrict CreateServiceLinkedRole and CreateRole policies to other
// accounts.

type Create struct {
	command
	alloc bool // TODO: Implement immediate allocation
}

func (cmd *Create) Run(ctx *Ctx, args []string) error {
	c := ctx.AWS()
	type info struct{ name, email string }
	in := make([]info, 0, len(args))
	for _, arg := range args {
		ne := strings.Split(arg, ":")
		if len(ne) != 2 || ne[0] == "" || strings.IndexByte(ne[1], '@') == -1 {
			usageErr(cmd, "invalid name:email combination %q", arg)
		}
		in = append(in, info{ne[0], ne[1]})
	}
	var created Accounts
	for _, v := range in {
		id, err := c.CreateAccount(v.name, v.email)
		created = append(created, &Account{
			Account: &awsgw.Account{
				ID:   id,
				Name: v.name,
			},
			Err: err,
		})
		if err == nil {
			// TODO: Figure out why this fails
			//time.Sleep(3 * time.Second)
			//err = c.CreateAdminRole(id, "")
		}
	}
	return cmd.PrintOutput(listResults(created))
}
