package internal

import (
	"bufio"
	"bytes"
	"errors"
	"strings"
	"testing"
	"unicode"

	"github.com/stretchr/testify/assert"
)

//noinspection GoInvalidCompositeLiteral (GoLand bug GO-5283)
func TestPrinter(t *testing.T) {
	tests := []struct {
		in  interface{}
		out string
	}{
		{
			in:  []*struct{}{},
			out: "",
		}, {
			in:  []*struct{ A string }{},
			out: "",
		}, {
			in: []*struct{ A string }{
				{""},
			},
			out: table(`
				A
				-

			`),
		}, {
			in: []*struct{ A string }{
				{"a"},
			},
			out: table(`
				A
				-
				a
			`),
		}, {
			in: []*struct{ A string }{
				{"ab"},
			},
			out: table(`
				A
				--
				ab
			`),
		}, {
			in: []*struct {
				A string `printer:"B"`
			}{
				{},
			},
			out: table(`
				B
				-

			`),
		}, {
			in: []*struct {
				A string `printer:",width=3"`
			}{
				{},
			},
			out: table(`
				A
				---

			`),
		}, {
			in: []*struct {
				A string `printer:",width=0"`
			}{
				{},
			},
			out: "",
		}, {
			in: []*struct {
				ABC string `printer:",width=1"`
			}{
				{},
			},
			out: table(`
				ABC
				---

			`),
		}, {
			in: []*struct {
				A string `printer:",width=1"`
			}{
				{"ab"},
			},
			out: table(`
				A
				-
				.
			`),
		}, {
			in: []*struct {
				A string `printer:",width=2"`
			}{
				{"abc"},
			},
			out: table(`
				A
				--
				..
			`),
		}, {
			in: []*struct {
				A string `printer:",width=3"`
			}{
				{"abc"},
			},
			out: table(`
				A
				---
				abc
			`),
		}, {
			in: []*struct {
				A string `printer:",width=4"`
			}{
				{"abcd"},
				{"abcde"},
			},
			out: table(`
				A
				----
				abcd
				a...
			`),
		}, {
			in: []*struct {
				ABCD string `printer:",width=1"`
			}{
				{"abcd"},
				{"abcde"},
			},
			out: table(`
				ABCD
				----
				abcd
				a...
			`),
		}, {
			in: []*struct{ A, B string }{
				{"", "b"},
			},
			out: table(`
				A  B
				-  -
				   b
			`),
		}, {
			in: []*struct{ A, B string }{
				{"a", "bcd"},
				{"ef", "g"},
			},
			out: table(`
				A   B
				--  ---
				a   bcd
				ef  g
			`),
		}, {
			in: []*struct {
				A   string `printer:",width=-1"`
				BCD int    `printer:",width=-1,last"`
				EFG error
			}{
				{"abc", 123, nil},
				{"d", 4567, nil},
			},
			out: table(`
				A  BCD
				-  ---
				.  123
				d  4567
			`),
		},
	}
	var buf bytes.Buffer
	bio := bufio.NewWriter(&buf)
	for _, test := range tests {
		NewPrinter(test.in).Print(bio, nil)
		bio.Flush()
		assert.Equal(t, test.out, buf.String())
		buf.Reset()
	}
}

func TestRowPrinter(t *testing.T) {
	in := []*row{
		{"a", "bcd", nil},
		{"e", "fgh", errors.New("fail")},
	}
	var buf bytes.Buffer
	NewPrinter(in).Print(&buf, nil)
	out := table(`
		A  C
		-  -
		a
		e    <ERROR: fail>
	`)
	assert.Equal(t, out, buf.String())
}

type row struct {
	A string
	B string `printer:",width=0"`
	C error  `printer:",width=1"`
}

func (r *row) PrintRow(p *Printer) {
	more := r.C != nil
	p.PrintCol(0, r.A, more)
	p.PrintCol(1, r.B, more)
	if r.C != nil {
		p.PrintErr(r.C.Error())
	}
}

func table(s string) string {
	return strings.TrimLeftFunc(Dedent(s), unicode.IsSpace)
}
