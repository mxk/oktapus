package cmd

import (
	"bufio"
	"os"

	"github.com/LuminalHQ/oktapus/op"
)

func init() {
	op.Register(&op.CmdInfo{
		Names:   []string{"account-spec"},
		Summary: "Show detailed help for account-spec argument",
		MaxArgs: -1,
		Hidden:  true,
		New:     func() op.Cmd { return specHelp{Name: "account-spec"} },
	})
}

type specHelp struct {
	Name
	noFlags
}

func (specHelp) Help(w *bufio.Writer) {
	op.WriteHelp(w, `
		Account Filtering Specifications

		account-spec is a comma-separated list of account IDs, names, or tags.
		The three classes cannot be mixed. If an account ID is specified, then
		all entries must be account IDs. If one of the entries matches an
		existing account name, then all entries must be account names.
		Otherwise, the spec is interpreted as a collection of tags, which may
		also contain owner filtering criteria.

		The general entry syntax is "[!]name[[!]=value]" where value is a
		boolean expression for tags (0/1, true/false, etc.) or a string for the
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

		  "!owner,mytag"
		      Matches free accounts with "mytag" set.

		  "owner=me"
		      Matches accounts owned by the current user.

		  "owner!=user1"
		      Matches accounts not owned by user1 (including free ones).

		  "owner,owner!=me"
		      Matches accounts allocated by other users.

		  "owner=user1,owner=user2"
		      Matches accounts owned by user1 or user2.

		  "err"
		      When listing accounts, include those that cannot be accessed or
		      are not managed by oktapus.
	`)
}

func (specHelp) Run(ctx *op.Ctx, args []string) error {
	buf := bufio.NewWriter(os.Stdout)
	specHelp{}.Help(buf)
	return buf.Flush()
}
