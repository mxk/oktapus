package internal

import (
	"bytes"
	"io/ioutil"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestTime(t *testing.T) {
	d := time.Now().Sub(Time())
	assert.True(t, d >= 0)
	assert.True(t, d <= 1500*time.Millisecond)

	SetTime(time.Unix(1, 0))
	assert.Equal(t, time.Unix(1, 0), Time())

	SetTime(time.Time{})
	d = time.Now().Sub(Time())
	assert.True(t, d >= 0)
	assert.True(t, d <= 1500*time.Millisecond)
}

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

func TestDedent(t *testing.T) {
	tests := []*struct {
		in  string
		out string
	}{
		{"", ""},
		{"\t", "\t"},
		{"\t\n", "\t\n"},
		{"\t\n\t", "\t\n\t"},
		{"\t\n\tA", "\t\nA"},
		{"\n\tA", "\nA"},
		{"\n\tA\nB", "\nA\nB"},
		{"\n\t\t\n\tA\n\t\tB", "\n\t\t\nA\n\tB"},
		{"A\n\t\tB\nC\n\tD\n\t\t\tE\n\t", "A\nB\nC\nD\n\tE\n"},
	}
	for _, test := range tests {
		assert.Equal(t, test.out, Dedent(test.in))
	}
}
