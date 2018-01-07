package cmd

import (
	"sort"
	"strings"

	"oktapus/internal"
)

// TODO: Document 'all' and 'mine' implicit tags. Create common account-spec
// help.

func init() {
	register(&List{command: command{
		name:    []string{"list", "ls"},
		summary: "List accounts",
		usage:   "[options] account-spec",
		maxArgs: 1,
		help:    "List accounts in the organization.", // TODO: Improve
	}})
}

type List struct{ command }

type ListOutput struct {
	AccountID   string
	Name        string
	Owner       string
	Description string
	Tags        string `printer:"last"`
	Error       string
}

func (cmd *List) Run(ctx *Ctx, args []string) error {
	cmd.PadArgs(&args)
	c := ctx.AWS()
	match, err := getAccounts(c, args[0])
	if err != nil {
		return err
	}
	return cmd.PrintOutput(cmd.out(match))
}

func (cmd *List) out(match []*Account) ([]*ListOutput, internal.PrintCfgFunc, internal.PrintFunc) {
	out := make([]*ListOutput, 0, len(match))
	for _, ac := range match {
		ctl := ac.Ctl()
		sort.Strings(ctl.Tags)
		out = append(out, &ListOutput{
			AccountID:   ac.ID,
			Name:        ac.Name,
			Owner:       ctl.Owner,
			Description: ctl.Desc,
			Tags:        strings.Join(ctl.Tags, ","),
			Error:       explainError(ac.Error()),
		})
	}
	return out, nil, cmd.print
}

func (*List) print(p *internal.Printer, v interface{}) {
	ac := v.(*ListOutput)
	if ac.Error == "" {
		internal.DefaultPrintFunc(p, v)
		return
	}
	p.PrintCol(0, ac.AccountID, true)
	p.PrintCol(1, ac.Name, true)
	p.PrintErr(ac.Error)
}
