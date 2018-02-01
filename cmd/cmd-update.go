package cmd

import (
	"bufio"
	"flag"
	"sort"
)

func init() {
	register(&cmdInfo{
		names:   []string{"update", "tag"},
		summary: "Update account tags and/or description",
		usage:   "[options] account-spec [tags]",
		minArgs: 1,
		maxArgs: 2,
		new:     func() Cmd { return &update{Name: "update"} },
	})
}

type update struct {
	Name
	PrintFmt
	Desc *string
	Spec string
	Tags string
}

func (cmd *update) Help(w *bufio.Writer) {
	writeHelp(w, `
		Update account tags and/or description.

		To set or clear tags, specify them as a comma-separated list after the
		account-spec. Use the '!' prefix to clear existing tags. You may need to
		escape the '!' character with a backslash, or quote the entire argument,
		to inhibit shell expansion.
	`)
	accountSpecHelp(w)
}

func (cmd *update) FlagCfg(fs *flag.FlagSet) {
	cmd.PrintFmt.FlagCfg(fs)
	StringPtrVar(fs, &cmd.Desc, "desc", "Set account `description`")
}

func (cmd *update) Run(ctx *Ctx, args []string) error {
	padArgs(cmd, &args)
	cmd.Spec, cmd.Tags = args[0], args[1]
	out, err := ctx.Call(cmd)
	if err == nil {
		err = cmd.Print(out)
	}
	return err
}

func (cmd *update) Call(ctx *Ctx) (interface{}, error) {
	acs, err := ctx.Accounts(cmd.Spec)
	if err != nil {
		return nil, err
	}
	tags := newAccountSpec(cmd.Tags, ctx.AWS().CommonRole)
	if cmd.Desc == nil && len(tags.idx) == 0 {
		usageErr(cmd, "either description or tags must be specified")
	}
	mod := acs[:0]
	for _, ac := range acs {
		if ac.Err == nil {
			if cmd.Desc != nil {
				ac.Desc = *cmd.Desc
			}
			ac.Tags = cmd.updateTags(ac.Tags, tags)
			mod = append(mod, ac)
		}
	}
	return listAccounts(mod.Save()), nil
}

func (cmd *update) updateTags(tags []string, s *accountSpec) []string {
	m := make(map[string]struct{}, len(tags)+len(s.spec))
	for _, tag := range tags {
		m[tag] = struct{}{} // TODO: Validate?
	}
	for _, tag := range s.spec {
		if !isTag(tag, true) {
			usageErr(cmd, "invalid tag %q", tag)
		} else if tag, _, neg := parseTag(tag); neg {
			delete(m, tag)
		} else {
			m[tag] = struct{}{}
		}
	}
	tags = make([]string, 0, len(m))
	for tag := range m {
		tags = append(tags, tag)
	}
	sort.Strings(tags)
	return tags
}
