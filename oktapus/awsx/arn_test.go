package awsx

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestARN(t *testing.T) {
	tests := []*struct {
		ARN       ARN
		Partition string
		Service   string
		Region    string
		Account   string
		Resource  string
		Type      string
		Path      string
		Name      string
	}{{
		ARN: NilARN,
	}, {
		ARN:       "arn:a:b:c:*:x",
		Partition: "a",
		Service:   "b",
		Region:    "c",
		Account:   "*",
		Resource:  "x",
		Name:      "x",
	}, {
		ARN:      "arn::::*:x:",
		Account:  "*",
		Resource: "x:",
		Type:     "x",
	}, {
		ARN:      "arn::::*::y",
		Account:  "*",
		Resource: ":y",
		Name:     "y",
	}, {
		ARN:      "arn::::*:x:y",
		Account:  "*",
		Resource: "x:y",
		Type:     "x",
		Name:     "y",
	}, {
		ARN:      "arn::::*:x/",
		Account:  "*",
		Resource: "x/",
		Type:     "x",
		Path:     "/",
	}, {
		ARN:      "arn::::*:/y",
		Account:  "*",
		Resource: "/y",
		Path:     "/",
		Name:     "y",
	}, {
		ARN:      "arn::::*:x/y",
		Account:  "*",
		Resource: "x/y",
		Type:     "x",
		Path:     "/",
		Name:     "y",
	}, {
		ARN:      "arn::::*:x/y/z",
		Account:  "*",
		Resource: "x/y/z",
		Type:     "x",
		Path:     "/y/",
		Name:     "z",
	}}
	for _, test := range tests {
		require.True(t, test.ARN.Valid(), "arn=%s", test.ARN)
		assert.Equal(t, test.Partition, test.ARN.Partition(), "arn=%s", test.ARN)
		assert.Equal(t, test.Service, test.ARN.Service(), "arn=%s", test.ARN)
		assert.Equal(t, test.Region, test.ARN.Region(), "arn=%s", test.ARN)
		assert.Equal(t, test.Account, test.ARN.Account(), "arn=%s", test.ARN)
		assert.Equal(t, test.Resource, test.ARN.Resource(), "arn=%s", test.ARN)
		assert.Equal(t, test.Type, test.ARN.Type(), "arn=%s", test.ARN)
		assert.Equal(t, test.Path, test.ARN.Path(), "arn=%s", test.ARN)
		assert.Equal(t, test.Name, test.ARN.Name(), "arn=%s", test.ARN)

		r := NewARN(test.Partition, test.Service, test.Region, test.Account, test.Resource)
		assert.Equal(t, test.ARN, r)

		assert.Equal(t, r, r.WithPartition(test.Partition))
		assert.Equal(t, r, r.WithService(test.Service))
		assert.Equal(t, r, r.WithRegion(test.Region))
		assert.Equal(t, r, r.WithAccount(test.Account))
		assert.Equal(t, r, r.WithResource(test.Resource))

		assert.Equal(t, r, r.With(NewARN(test.Partition, "", test.Region, "", test.Resource)))
		assert.Equal(t, r, r.With(NewARN("", test.Service, "", test.Account, "")))

		if test.Path != "" {
			r := NewARN("", "", "", test.Account, test.Type, test.Path, test.Name)
			assert.Equal(t, test.ARN, r)
		}
	}
}

func TestARNInvalid(t *testing.T) {
	assert.False(t, arnPrefix.Valid())
	assert.False(t, ARN("").Valid())
	assert.False(t, ARN("arn::::").Valid())
	assert.Panics(t, func() { arnPrefix.Partition() })
	assert.Panics(t, func() { ARN("").Type() })
	assert.Panics(t, func() { ARN("").Path() })
	assert.Panics(t, func() { ARN("").Name() })
	assert.Panics(t, func() { ARN("arn::::").Resource() })
}

func TestCleanPath(t *testing.T) {
	tests := []*struct{ in, out string }{
		{in: "", out: ""},
		{in: "a", out: "/a"},
		{in: "/", out: ""},
		{in: "a/", out: "/a"},
		{in: "/a", out: "/a"},
		{in: "/a/", out: "/a"},
		{in: "a/b", out: "/a/b"},
		{in: "//a/b", out: "/a/b"},
		{in: "a/./b//c/", out: "/a/b/c"},
	}
	for _, test := range tests {
		path := string(cleanPath(test.in))
		assert.Equal(t, test.out, path, "in=%q", test.in)
	}
}
