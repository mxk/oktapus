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
		new:     func() Cmd { return &Update{Name: "update"} },
	})
}

type Update struct {
	Name
	PrintFmt
	desc *string
}

func (cmd *Update) Help(w *bufio.Writer) {
	writeHelp(w, `
		Update account tags and/or description.

		To set or clear tags, specify them as a comma-separated list after the
		account-spec. Use the '!' prefix to clear existing tags. You may need to
		escape the '!' character with a backslash, or quote the entire argument,
		to inhibit shell expansion.
	`)
	accountSpecHelp(w)
}

func (cmd *Update) FlagCfg(fs *flag.FlagSet) {
	cmd.PrintFmt.FlagCfg(fs)
	StringPtrVar(fs, &cmd.desc, "desc", "Set account description")
}

func (cmd *Update) Run(ctx *Ctx, args []string) error {
	padArgs(cmd, &args)
	match, err := ctx.Accounts(args[0])
	if err != nil {
		return err
	}
	tags := newAccountSpec(args[1], ctx.AWS().CommonRole)
	if cmd.desc == nil && len(tags.idx) == 0 {
		usageErr(cmd, "either description or tags must be specified")
	}
	mod := match[:0]
	for _, ac := range match {
		if ac.Err == nil {
			if cmd.desc != nil {
				ac.Desc = *cmd.desc
			}
			ac.Tags = cmd.updateTags(ac.Tags, tags)
			mod = append(mod, ac)
		}
	}
	return cmd.Print(listAccounts(mod.Save()))
}

func (cmd *Update) updateTags(tags []string, s *accountSpec) []string {
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
