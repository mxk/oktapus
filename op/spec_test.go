package op

import (
	"strings"
	"testing"

	"github.com/LuminalHQ/oktapus/awsgw"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOwner(t *testing.T) {
	all := accounts{
		{"1", ""},
		{"2", "a"},
		{"3", "b"},
		{"4", "c"},
	}.get()
	tests := []*struct{ spec, want string }{{
		spec: "",
		want: "1,2,3,4",
	}, {
		spec: "owner",
		want: "2,3,4",
	}, {
		spec: "!owner",
		want: "1",
	}, {
		spec: "!owner,owner",
		want: "2,3,4",
	}, {
		spec: "owner,!owner",
		want: "1",
	}, {
		spec: "owner=a",
		want: "2",
	}, {
		spec: "owner!=a",
		want: "1,3,4",
	}, {
		spec: "owner!=a,owner=a",
		want: "2",
	}, {
		spec: "owner=a,owner!=a",
		want: "1,3,4",
	}, {
		spec: "owner,owner=a",
		want: "2,3,4",
	}, {
		spec: "owner,owner!=a",
		want: "3,4",
	}, {
		spec: "!owner,owner=a",
		want: "1,2",
	}, {
		spec: "!owner,owner!=a",
		want: "1",
	}, {
		spec: "owner=a,owner=b",
		want: "2,3",
	}, {
		spec: "owner=a,owner!=b",
		want: "1,2,4",
	}, {
		spec: "owner!=a,owner=b",
		want: "1,3,4",
	}, {
		spec: "owner!=a,owner!=b",
		want: "1,4",
	}, {
		spec: "owner,owner=a,owner!=b",
		want: "2,4",
	}, {
		spec: "!owner,owner=a,owner!=b",
		want: "1,2",
	}, {
		spec: "owner=me",
		want: "2",
	}, {
		spec: "owner=me,owner!=a",
		want: "1,3,4",
	}}
	ids := make([]string, 0, len(all))
	for _, test := range tests {
		match, err := ParseAccountSpec(test.spec, "a").Filter(all)
		require.NoError(t, err)
		ids = ids[:0]
		for _, ac := range match {
			ids = append(ids, ac.ID)
		}
		assert.Equal(t, test.want, strings.Join(ids, ","), "spec=%q", test.spec)
	}

	match, err := ParseAccountSpec("owner=me", "").Filter(all)
	require.NoError(t, err)
	assert.Empty(t, match)

	match, err = ParseAccountSpec("owner!=me", "").Filter(all)
	require.NoError(t, err)
	assert.Equal(t, all, match)
}

type accounts []struct{ id, owner string }

func (acs accounts) get() Accounts {
	all := make(Accounts, len(acs))
	for i, ac := range acs {
		all[i] = &Account{
			Account: &awsgw.Account{ID: ac.id},
			Ctl:     &Ctl{Owner: ac.owner},
		}
	}
	return all
}
