package cmd

import (
	"flag"
	"sort"
)

func init() {
	register(&Update{command: command{
		name:    []string{"update", "tag"},
		summary: "Update account tags and/or description",
		usage:   "[options] account-spec [tags]",
		minArgs: 1,
		maxArgs: 2,
	}})
}

type Update struct {
	command
	desc string
}

func (cmd *Update) FlagCfg(fs *flag.FlagSet) {
	cmd.command.FlagCfg(fs)
	fs.StringVar(&cmd.desc, "desc", "", "Set account description")
}

func (cmd *Update) Run(ctx *Ctx, args []string) error {
	cmd.PadArgs(&args)
	c := ctx.AWS()
	match, err := getAccounts(c, args[0])
	if err != nil {
		return err
	}
	setDesc := cmd.HaveOpt("desc")
	tags := newAccountSpec(args[1], c.CommonRole)
	if !setDesc && len(tags.idx) == 0 {
		usageErr(cmd, "either description or tags must be specified")
	}
	for _, ac := range match {
		ctl := ac.Ctl()
		if setDesc {
			ctl.Desc = cmd.desc
		}
		ctl.Tags = cmd.updateTags(ctl.Tags, tags)
	}
	for _, ac := range match {
		ac.Save()
	}
	return cmd.PrintOutput(cmds["list"].(*List).out(match))
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
