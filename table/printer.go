package table

import (
	"bufio"
	"fmt"
	"io"
	"reflect"
	"strconv"
	"strings"
)

// PrintRowFunc is a custom function called to print each table row.
type PrintRowFunc func(p *Printer, row interface{})

// RowPrinter may be implemented by the type being printed to control its own
// output.
type RowPrinter interface {
	PrintRow(p *Printer)
}

// Printer outputs data in a table format.
type Printer struct {
	*bufio.Writer

	Cols   []Col
	ColSep string

	pad string
	v   interface{}
}

// Col contains printer column information.
type Col struct {
	Name       string
	Width      int
	FixedWidth bool
}

// NewPrinter creates a table layout printer for v. The only currently supported
// data type is a slice of struct pointers.
func NewPrinter(v interface{}) *Printer {
	p := &Printer{Cols: colInfo(v), ColSep: "  ", v: v}
	if len(p.Cols) == 0 {
		return p
	}

	// Calculate column widths unless all are fixed
	for i := range p.Cols {
		if !p.Cols[i].FixedWidth {
			p.Print(nil, calcWidths)
			break
		}
	}

	// Determine the maximum padding that may be needed
	pad := 0
	for i := range p.Cols {
		w := p.Cols[i].Width
		if w < 0 {
			w = len(p.Cols[i].Name)
		}
		if pad < w {
			pad = w
		}
	}
	p.pad = strings.Repeat(" ", pad)
	return p
}

// Print outputs printer data to w. If fn is specified, it is called to print
// each row.
func (p *Printer) Print(w io.Writer, fn PrintRowFunc) {
	if bio, ok := w.(*bufio.Writer); ok || w == nil {
		p.Writer = bio
	} else {
		p.Writer = bufio.NewWriter(w)
		defer p.Flush()
	}
	v := reflect.ValueOf(p.v)
	n := v.Len()
	if n == 0 || len(p.Cols) == 0 {
		return
	}

	// Column names and separators
	if p.Writer != nil {
		if len(p.pad) == 0 {
			return // All columns have zero width
		}
		last := len(p.Cols) - 1
		for i := range p.Cols {
			p.PrintCol(i, p.Cols[i].Name, i < last)
		}
		p.WriteByte('\n')
		sep := strings.Repeat("-", len(p.pad))
		for i := range p.Cols {
			w := p.Cols[i].Width
			if w < 0 {
				w = len(p.Cols[i].Name)
			}
			p.PrintCol(i, sep[:w], i < last)
		}
		p.WriteByte('\n')
	}

	// Rows
	if fn == nil {
		iface := reflect.TypeOf((*RowPrinter)(nil)).Elem()
		if reflect.PtrTo(structType(p.v)).Implements(iface) {
			fn = rowPrinter
		} else {
			fn = PrintRow
		}
	}
	for i := 0; i < n; i++ {
		if fn(p, v.Index(i).Interface()); p.Writer != nil {
			p.WriteByte('\n')
		}
	}
}

// PrintCol prints s as the ith column, adding padding and column separator if
// more is true.
func (p *Printer) PrintCol(i int, s string, more bool) {
	w := p.Cols[i].Width
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
	p.WriteString("  <ERROR: ")
	p.WriteString(e)
	p.WriteByte('>')
}

// PrintRow uses reflection to print each row.
func PrintRow(p *Printer, row interface{}) {
	v := reflect.ValueOf(row).Elem()
	last := len(p.Cols) - 1
	for i := range p.Cols {
		p.PrintCol(i, valStr(v.Field(i)), i < last)
	}
}

// rowPrinter uses the RowPrinter interface to print each row.
func rowPrinter(p *Printer, row interface{}) {
	row.(RowPrinter).PrintRow(p)
}

// colInfo returns column information for struct type in v.
func colInfo(v interface{}) []Col {
	s := structType(v)
	cols := make([]Col, s.NumField())
	for i := range cols {
		f, c := s.Field(i), &cols[i]
		c.Name, c.Width = f.Name, len(f.Name)
		if c.parseTags(f.Tag.Get("printer")) {
			return cols[:i+1]
		} else if c.Width < 0 && i < len(cols)-1 {
			// Only the last column may have an unlimited width
			c.Width = len(f.Name)
		}
	}
	return cols
}

// parseTags updates column information based on struct field tags. It returns
// true if c is the last column.
func (c *Col) parseTags(tags string) bool {
	var next string
	name, last := true, false
	for tags != "" {
		if i := strings.IndexByte(tags, ','); i == -1 {
			next, tags = tags, ""
		} else {
			next, tags = tags[:i], tags[i+1:]
		}
		if name {
			if name = false; next != "" {
				c.Name = next
			}
		} else if next == "last" {
			last = true
		} else if w := strings.TrimPrefix(next, "width="); w != next {
			if i, _ := strconv.Atoi(w); i <= 0 || c.Width < i {
				c.Width = i
			}
			c.FixedWidth = true
		}
	}
	return last
}

// calcWidths is a PrintRowFunc that calculates column widths.
func calcWidths(p *Printer, row interface{}) {
	v := reflect.ValueOf(row).Elem()
	for i := range p.Cols {
		if c := &p.Cols[i]; !c.FixedWidth {
			if n := len(valStr(v.Field(i))); c.Width < n {
				c.Width = n
			}
		}
	}
}

// structType extracts the underlying struct type from t.
func structType(v interface{}) reflect.Type {
	t := reflect.TypeOf(v)
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
