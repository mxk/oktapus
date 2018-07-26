package internal

import (
	"bytes"
	"io/ioutil"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCloseBody(t *testing.T) {
	b := bytes.NewReader(make([]byte, 4097))
	CloseBody(ioutil.NopCloser(b))
	assert.Equal(t, 1, b.Len())
}

func TestJSON(t *testing.T) {
	assert.Equal(t, "{}\n", JSON(struct{}{}))
}

func TestStringsEq(t *testing.T) {
	tests := []*struct {
		a, b []string
		eq   bool
	}{
		{[]string{}, []string{}, true},
		{[]string{"a"}, []string{}, false},
		{[]string{"a"}, []string{"b", "c"}, false},
		{[]string{"a"}, []string{"b"}, false},
		{[]string{"a"}, []string{"a"}, true},
		{[]string{"a", "b"}, []string{"a", "B"}, false},
		{[]string{"a", "b"}, []string{"a", "b"}, true},
	}
	for _, test := range tests {
		assert.Equal(t, test.eq, StringsEq(test.a, test.b),
			"a=%v b=%v", test.a, test.b)
	}
}
