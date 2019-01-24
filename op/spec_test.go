package op

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/mxk/oktapus/mock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStatic(t *testing.T) {
	all := accounts{
		{id: "1", name: "a"},
		{id: "2", name: "b"},
		{id: "3", name: "c"},
		{id: "4", name: "d"},
	}.get()
	tests := []*struct{ spec, want string }{{
		spec: "000000000001",
		want: "1",
	}, {
		spec: "000000000002,000000000004",
		want: "2,4",
	}, {
		spec: "a",
		want: "1",
	}, {
		spec: "d,c",
		want: "3,4",
	}, {
		spec: "a" + strings.Repeat(",c", 64),
		want: "1,3",
	}}
	for _, test := range tests {
		match, err := ParseAccountSpec(test.spec, "").Filter(all)
		require.NoError(t, err)
		assert.Equal(t, test.want, getIDs(match), "spec=%q", test.spec)
	}

	_, err := ParseAccountSpec("000000000042", "").Filter(all)
	assert.EqualError(t, err, `account id "000000000042" not found`)

	_, err = ParseAccountSpec("c,x", "").Filter(all)
	assert.EqualError(t, err, `account name "x" not found`)

	_, err = ParseAccountSpec("x"+strings.Repeat(",x", 64), "").Filter(all)
	assert.EqualError(t, err, `account name "x" not found`)

	_, err = ParseAccountSpec("!c", "").Filter(all)
	assert.EqualError(t, err, `account name "c" cannot be negated`)
}

func TestDynamic(t *testing.T) {
	all := accounts{
		{id: "1", tags: "a"},
		{id: "2", tags: "b"},
		{id: "3", tags: "a,c"},
		{id: "4", tags: "b,d"},
		{id: "5", err: "not initialized"},
	}.get()
	tests := []*struct{ spec, want string }{{
		spec: "",
		want: "1,2,3,4",
	}, {
		spec: tagAll,
		want: "1,2,3,4,5",
	}, {
		spec: "x",
		want: "",
	}, {
		spec: "x,all=true",
		want: "5",
	}, {
		spec: "x,all,all=0",
		want: "",
	}, {
		spec: "a",
		want: "1,3",
	}, {
		spec: "!a",
		want: "2,4",
	}, {
		spec: "a,!a",
		want: "",
	}, {
		spec: "a,!c",
		want: "1",
	}, {
		spec: "c",
		want: "3",
	}, {
		spec: "!a,d,all",
		want: "4,5",
	}}
	for _, test := range tests {
		match, err := ParseAccountSpec(test.spec, "").Filter(all)
		require.NoError(t, err)
		assert.Equal(t, test.want, getIDs(match), "spec=%q", test.spec)
	}
}

func TestOwner(t *testing.T) {
	all := accounts{
		{id: "1", owner: ""},
		{id: "2", owner: "a"},
		{id: "3", owner: "b"},
		{id: "4", owner: "c"},
	}.get()
	tests := []*struct{ spec, want string }{{
		spec: "",
		want: "1,2,3,4",
	}, {
		spec: tagOwner,
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
	for _, test := range tests {
		match, err := ParseAccountSpec(test.spec, "a").Filter(all)
		require.NoError(t, err)
		assert.Equal(t, test.want, getIDs(match), "spec=%q", test.spec)
	}

	match, err := ParseAccountSpec("owner=me", "").Filter(all)
	require.NoError(t, err)
	assert.Empty(t, match)

	match, err = ParseAccountSpec("owner!=me", "").Filter(all)
	require.NoError(t, err)
	assert.Equal(t, all, match)
}

type accounts []*struct{ id, name, owner, tags, err string }

func (acs accounts) get() Accounts {
	all := make(Accounts, len(acs))
	for i, ac := range acs {
		tags, _, err := ParseTags(ac.tags)
		if err != nil {
			panic(err)
		}
		n := NewAccount(mock.AccountID(ac.id), ac.name)
		if ac.err == "" {
			n.Ctl = Ctl{Owner: ac.owner, Tags: tags}
		} else {
			n.Err = errors.New(ac.err)
		}
		n.CtlUpdate(n.Err)
		all[i] = n
	}
	return all
}

func getIDs(acs Accounts) string {
	var buf bytes.Buffer
	for i, ac := range acs {
		if i > 0 {
			buf.WriteByte(',')
		}
		buf.WriteString(strings.TrimLeft(ac.ID, "0"))
	}
	return buf.String()
}
