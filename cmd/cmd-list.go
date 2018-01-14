package cmd

import (
	"sort"
	"strings"

	"github.com/LuminalHQ/oktapus/internal"
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
	Tags        string `printer:",last"`
	Error       string
}

func (cmd *List) Run(ctx *Ctx, args []string) error {
	cmd.PadArgs(&args)
	match, err := ctx.Accounts(args[0])
	if err != nil {
		return err
	}
	return cmd.PrintOutput(listAccounts(match))
}

func listAccounts(acs Accounts) []*ListOutput {
	out := make([]*ListOutput, 0, len(acs))
	var null Ctl
	for _, ac := range acs {
		ctl := ac.Ctl
		if ctl == nil {
			ctl = &null
		}
		sort.Strings(ctl.Tags)
		out = append(out, &ListOutput{
			AccountID:   ac.ID,
			Name:        ac.Name,
			Owner:       ctl.Owner,
			Description: ctl.Desc,
			Tags:        strings.Join(ctl.Tags, ","),
			Error:       explainError(ac.Err),
		})
	}
	return out
}

func (o *ListOutput) PrintRow(p *internal.Printer) {
	if o.Error == "" {
		internal.PrintRow(p, o)
	} else {
		p.PrintCol(0, o.AccountID, true)
		p.PrintCol(1, o.Name, true)
		p.PrintErr(o.Error)
	}
}
