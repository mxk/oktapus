package op

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseTags(t *testing.T) {
	tests := []*struct {
		tags, set, clr string
	}{{
		tags: "",
		set:  "",
		clr:  "",
	}, {
		tags: "a",
		set:  "a",
		clr:  "",
	}, {
		tags: "!b",
		set:  "",
		clr:  "b",
	}, {
		tags: "a,!b",
		set:  "a",
		clr:  "b",
	}, {
		tags: "!d,c,!b,a",
		set:  "a,c",
		clr:  "b,d",
	}}
	for _, test := range tests {
		set, clr, err := ParseTags(test.tags)
		require.NoError(t, err)
		assert.Equal(t, test.set, strings.Join(set, ","), "tags=%q", test.tags)
		assert.Equal(t, test.clr, strings.Join(clr, ","), "tags=%q", test.tags)
	}
	for _, tags := range []string{",", "x=y", "err", "a*", "*", "!"} {
		_, _, err := ParseTags(tags)
		assert.Error(t, err, "tags=%q", tags)
	}
}

func TestDiff(t *testing.T) {
	tests := []*struct {
		t, u, set, clr string
	}{{
		t:   "",
		u:   "",
		set: "",
		clr: "",
	}, {
		t:   "a",
		u:   "a",
		set: "",
		clr: "",
	}, {
		t:   "a",
		u:   "b",
		set: "a",
		clr: "b",
	}, {
		t:   "a,b",
		u:   "a",
		set: "b",
		clr: "",
	}, {
		t:   "a",
		u:   "a,b",
		set: "",
		clr: "b",
	}, {
		t:   "a,c",
		u:   "a,b",
		set: "c",
		clr: "b",
	}, {
		t:   "a,b,d",
		u:   "a,c,d",
		set: "b",
		clr: "c",
	}}
	for _, test := range tests {
		g, _, err := ParseTags(test.t)
		require.NoError(t, err)
		u, _, err := ParseTags(test.u)
		require.NoError(t, err)
		set, clr := g.Diff(u)
		assert.Equal(t, test.set, strings.Join(set, ","), "t=%q u=%q", test.t, test.u)
		assert.Equal(t, test.clr, strings.Join(clr, ","), "t=%q u=%q", test.t, test.u)

		u.Apply(set, clr)
		assert.Equal(t, test.t, strings.Join(u, ","), "t=%q u=%q", test.t, test.u)
	}
}

func TestApply(t *testing.T) {
	tests := []*struct {
		t, set, clr, u string
	}{{
		t:   "",
		set: "",
		clr: "",
		u:   "",
	}, {
		t:   "",
		set: "a",
		clr: "",
		u:   "a",
	}, {
		t:   "a",
		set: "",
		clr: "a",
		u:   "",
	}, {
		t:   "a",
		set: "a",
		clr: "b",
		u:   "a",
	}, {
		t:   "a,c,d",
		set: "b",
		clr: "c",
		u:   "a,b,d",
	}, {
		t:   "a,c,d",
		set: "d,c,b,a",
		clr: "d,c,b,a",
		u:   "a,b,c,d",
	}}
	for _, test := range tests {
		u, _, err := ParseTags(test.t)
		require.NoError(t, err)
		set, _, err := ParseTags(test.set)
		require.NoError(t, err)
		clr, _, err := ParseTags(test.clr)
		require.NoError(t, err)
		u.Apply(set, clr)
		assert.Equal(t, test.u, strings.Join(u, ","), "t=%q set=%q clr=%q",
			test.t, test.set, test.clr)
	}
}
