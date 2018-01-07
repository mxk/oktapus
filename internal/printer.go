package internal

import (
	"bufio"
	"fmt"
	"reflect"
	"strings"
)

type (
	PrintCfgFunc func(p *Printer)
	PrintFunc    func(p *Printer, row interface{})
)

// Printer formats data into a table layout.
type Printer struct {
	*bufio.Writer

	Cols   []string
	Widths []int
	ColSep string

	pad string
	v   interface{}
}

// NewPrinter creates a table layout printer for v. The only currently supported
// data type is a slice of struct pointers.
func NewPrinter(v interface{}, cfg PrintCfgFunc) *Printer {
	cols := colNames(v)
	p := &Printer{
		Cols:   cols,
		Widths: make([]int, len(cols)),
		ColSep: "  ",
		v:      v,
	}
	for i, col := range cols {
		p.Widths[i] = len(col)
	}
	p.Print(nil, calcWidths)
	if cfg != nil {
		cfg(p)
	}

	// Determine the maximum padding that may be needed
	pad := 0
	for i, w := range p.Widths {
		if n := len(cols[i]); w < n && w != 0 {
			if w = n; w > 0 || i < len(cols)-1 {
				// Increase width to accommodate column name. Only the last
				// column is allowed have an unlimited width (-1).
				p.Widths[i] = w
			}
		}
		if pad < w {
			pad = w
		}
	}
	p.pad = strings.Repeat(" ", pad)
	return p
}

// ColIdx returns the index of the named column or -1 if there is no column with
// that name. PrintFuncs should not call this method for performance reasons.
func (p *Printer) ColIdx(name string) int {
	for i, col := range p.Cols {
		if col == name {
			return i
		}
	}
	return -1
}

// Print outputs printer data to w. If fn is specified, it is called to print
// each row.
func (p *Printer) Print(w *bufio.Writer, fn PrintFunc) {
	p.Writer = w
	v := reflect.ValueOf(p.v)
	n := v.Len()
	if n == 0 || len(p.Cols) == 0 {
		return
	}

	// Column names and separators
	if w != nil {
		last := len(p.Cols) - 1
		for i, col := range p.Cols {
			p.PrintCol(i, col, i < last)
		}
		w.WriteByte('\n')
		sep := strings.Repeat("-", len(p.pad))
		for i, w := range p.Widths {
			if w < 0 {
				w = len(p.Cols[i])
			}
			p.PrintCol(i, sep[:w], i < last)
		}
		w.WriteByte('\n')
	}

	// Rows
	if fn == nil {
		fn = DefaultPrintFunc
	}
	for i := 0; i < n; i++ {
		if fn(p, v.Index(i).Interface()); w != nil {
			w.WriteByte('\n')
		}
	}
}

// PrintCol prints s as the ith column, adding padding and column separator if
// more is true.
func (p *Printer) PrintCol(i int, s string, more bool) {
	w := p.Widths[i]
	if w == 0 {
		return
	}
	if len(s) <= w || w < 0 {
		p.WriteString(s)
	} else if w > 3 {
		p.WriteString(s[:w-3])
		p.WriteString("...")
	} else {
		p.WriteString("..."[:w])
	}
	if more {
		if n := w - len(s); n > 0 {
			p.WriteString(p.pad[:n])
		}
		p.WriteString(p.ColSep)
	}
}

// PrintErr prints error string e as the last column in the current row.
func (p *Printer) PrintErr(e string) {
	p.WriteString("  <error: ")
	p.WriteString(e)
	p.WriteByte('>')
}

// DefaultPrintFunc is used to print each table row when a custom row function
// is not specified.
func DefaultPrintFunc(p *Printer, row interface{}) {
	v := reflect.ValueOf(row).Elem()
	last := len(p.Cols) - 1
	for i := range p.Cols {
		p.PrintCol(i, valStr(v.Field(i)), i < last)
	}
}

// colNames returns column names for type t.
func colNames(v interface{}) []string {
	t := structType(reflect.TypeOf(v))
	f := make([]string, t.NumField())
	for i := range f {
		sf := t.Field(i)
		if f[i] = sf.Name; sf.Tag.Get("printer") == "last" {
			f = f[:i+1]
			break
		}
	}
	return f
}

// calcWidths is a PrintFunc that calculates column widths.
func calcWidths(p *Printer, row interface{}) {
	v := reflect.ValueOf(row).Elem()
	for i, max := range p.Widths {
		if n := len(valStr(v.Field(i))); max < n {
			p.Widths[i] = n
		}
	}
}

// structType extracts the underlying struct type from t.
func structType(t reflect.Type) reflect.Type {
	for t.Kind() != reflect.Struct {
		t = t.Elem()
	}
	return t
}

// valStr returns the string representation of v.
func valStr(v reflect.Value) string {
	if v.Kind() == reflect.String {
		return v.String()
	}
	return fmt.Sprint(v)
}
