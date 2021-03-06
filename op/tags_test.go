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
		assert.Equal(t, test.set, set.Sort().String(), "tags=%q", test.tags)
		assert.Equal(t, test.clr, clr.String(), "tags=%q", test.tags)
	}
	for _, tags := range []string{",", "x=y", "a*", "*", "!", tagAll, tagOwner} {
		_, _, err := ParseTags(tags)
		assert.Error(t, err, "tags=%q", tags)
	}
}

func TestTagsDiff(t *testing.T) {
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
		assert.Equal(t, test.set, set.String(), "t=%q u=%q", test.t, test.u)
		assert.Equal(t, test.clr, clr.String(), "t=%q u=%q", test.t, test.u)

		u.Apply(set, clr)
		assert.Equal(t, test.t, strings.Join(u, ","), "t=%q u=%q", test.t, test.u)
	}
}

func TestTagsApply(t *testing.T) {
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
		assert.Equal(t, test.u, u.String(), "t=%q set=%q clr=%q",
			test.t, test.set, test.clr)
	}
}

func TestTagsEq(t *testing.T) {
	tests := []*struct {
		a, b Tags
		eq   bool
	}{
		{Tags{}, Tags{}, true},
		{Tags{"a"}, Tags{}, false},
		{Tags{"a"}, Tags{"b", "c"}, false},
		{Tags{"a"}, Tags{"b"}, false},
		{Tags{"a"}, Tags{"a"}, true},
		{Tags{"a", "b"}, Tags{"a", "B"}, false},
		{Tags{"a", "b"}, Tags{"a", "b"}, true},
	}
	for _, tc := range tests {
		assert.Equal(t, tc.eq, tc.a.eq(tc.b), "%+v", tc)
	}
}
