package internal

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"sync"
)

var Log = log{os.Stderr}

var logMu sync.Mutex
var bufs = sync.Pool{New: func() interface{} {
	return new(bytes.Buffer)
}}

type log struct{ w io.Writer }

func (l log) D(format string, v ...interface{}) { l.out('D', format, v...) }
func (l log) I(format string, v ...interface{}) { l.out('I', format, v...) }
func (l log) W(format string, v ...interface{}) { l.out('W', format, v...) }
func (l log) E(format string, v ...interface{}) { l.out('E', format, v...) }
func (l log) F(format string, v ...interface{}) {
	l.out('F', format, v...)
	os.Exit(1)
}

func (l log) out(lvl byte, format string, v ...interface{}) {
	b := bufs.Get().(*bytes.Buffer)
	b.Reset()
	b.WriteByte('[')
	b.WriteByte(lvl)
	b.WriteString("] ")
	fmt.Fprintf(b, format, v...)
	if n := len(format); n > 0 && format[n-1] != '\n' {
		b.WriteByte('\n')
	}
	logMu.Lock()
	defer logMu.Unlock()
	l.w.Write(b.Bytes())
	bufs.Put(b)
}
