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
	c := ctx.AWS()
	match, err := getAccounts(c, args[0])
	if err != nil {
		return err
	}
	out := make([]*ListOutput, 0, len(match))
	for _, ac := range match {
		out = append(out, newListOutput(ac, nil))
	}
	return cmd.PrintOutput(out)
}

func newListOutput(ac *Account, err error) *ListOutput {
	ctl := ac.Ctl()
	if err == nil {
		err = ac.Error()
	}
	sort.Strings(ctl.Tags)
	return &ListOutput{
		AccountID:   ac.ID,
		Name:        ac.Name,
		Owner:       ctl.Owner,
		Description: ctl.Desc,
		Tags:        strings.Join(ctl.Tags, ","),
		Error:       explainError(err),
	}
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
