package cmd

import (
	"flag"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestStringPtrVar(t *testing.T) {
	ptr := func(s string) *string { return &s }
	tests := []struct {
		args []string
		want *string
	}{
		{[]string{}, nil},
		{[]string{"-s="}, ptr("")},
		{[]string{"-s=x"}, ptr("x")},
		{[]string{"-s=x", "-s=yz"}, ptr("yz")},
	}
	for _, test := range tests {
		fs := flag.NewFlagSet("", flag.PanicOnError)
		var s *string
		StringPtrVar(fs, &s, "s", "")
		fs.Parse(test.args)
		assert.Equal(t, test.want, s)
	}
}
