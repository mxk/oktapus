package cmd

import (
	"bufio"
	"flag"
	"os"
)

func init() {
	register(&cmdInfo{
		names:   []string{"account-spec"},
		summary: "Show detailed help for account-spec argument",
		maxArgs: -1,
		hidden:  true,
		new:     func() Cmd { return specHelp{} },
	})
}

type specHelp struct{}

func (specHelp) Info() *cmdInfo           { return cmds["account-spec"] }
func (specHelp) Help(w *bufio.Writer)     { accountSpecLongHelp(w) }
func (specHelp) FlagCfg(fs *flag.FlagSet) {}

func (specHelp) Run(ctx *Ctx, args []string) error {
	buf := bufio.NewWriter(os.Stdout)
	defer buf.Flush()
	accountSpecLongHelp(buf)
	return nil
}

func accountSpecHelp(w *bufio.Writer) {
	w.WriteByte('\n')
	writeHelp(w, `
		Run 'oktapus help account-spec' for details on account filtering
		specifications.
	`)
}

func accountSpecLongHelp(w *bufio.Writer) {
	writeHelp(w, `
		Account Filtering Specifications

		account-spec is a comma-separated list of account IDs, names, or tags.
		The three classes cannot be mixed. If an account ID is specified, then
		all entries must be account IDs. If one of the entries matches an
		existing account name, then all entries must be account names.
		Otherwise, the spec is interpreted as a collection of tags, which may
		also contain owner filtering criteria.

		The general entry syntax is "[!]name[[!]=value]" where value is a
		boolean expression for tags (0/1, true/false, etc.) or a string for
		owner spec.

		Tag specification examples:

		"mytag" or "mytag=true"
			Matches accounts with "mytag" set.

		"!othertag" or "othertag=false"
			Matches accounts with "othertag" not set.

		"mytag,!othertag"
			Matches accounts with "mytag" set and "othertag" not set.

		"owner"
			Matches allocated accounts.

		"!owner"
			Matches free accounts.

		"owner=me"
			Matches accounts owned by the current user.

		"owner=user1@example.com,owner=user2@example.com"
			Matches accounts owned by the specified users.

		"owner,owner!=me"
			Matches accounts allocated by other users.

		"err"
			When listing accounts, include those that cannot be accessed or are
			not managed by oktapus.
	`)
}
