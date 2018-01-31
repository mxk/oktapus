package internal

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"sync"
)

var Log = &log{w: os.Stderr}

type LogMsg struct {
	Level byte
	Msg   string
}

type log struct {
	w io.Writer
	f LogFunc
}

var logMu sync.Mutex
var bufs = sync.Pool{New: func() interface{} {
	return new(bytes.Buffer)
}}

// LogFunc is a function called for each log message. The logger guarantees that
// the function will never be called concurrently.
type LogFunc func(m *LogMsg)

// SetWriter sets log output writer. If w is nil, logging to a writer is
// disabled.
func (l *log) SetWriter(w io.Writer) (prev io.Writer) {
	logMu.Lock()
	defer logMu.Unlock()
	prev, l.w = l.w, w
	return prev
}

// SetFunc sets log output function. If fn is nil, logging to a function is
// disabled.
func (l *log) SetFunc(fn LogFunc) (prev LogFunc) {
	logMu.Lock()
	defer logMu.Unlock()
	prev, l.f = l.f, fn
	return prev
}

func (l *log) D(format string, v ...interface{}) { l.out('D', format, v...) }
func (l *log) I(format string, v ...interface{}) { l.out('I', format, v...) }
func (l *log) W(format string, v ...interface{}) { l.out('W', format, v...) }
func (l *log) E(format string, v ...interface{}) { l.out('E', format, v...) }
func (l *log) F(format string, v ...interface{}) {
	l.out('F', format, v...)
	os.Exit(1)
}

func (l *log) Msg(m *LogMsg) { l.out(m.Level, "%s", m.Msg) }

func (l *log) out(lvl byte, format string, v ...interface{}) {
	b := bufs.Get().(*bytes.Buffer)
	b.Reset()
	b.WriteByte('[')
	b.WriteByte(lvl)
	b.WriteString("] ")
	si := b.Len()
	fmt.Fprintf(b, format, v...)
	if n := len(format); n > 0 && format[n-1] != '\n' {
		b.WriteByte('\n')
	}
	var m *LogMsg
	if l.f != nil {
		m = &LogMsg{
			Level: lvl,
			Msg:   string(b.Bytes()[si:]),
		}
	}
	logMu.Lock()
	defer logMu.Unlock()
	if m != nil {
		l.f(m)
	}
	if l.w != nil {
		l.w.Write(b.Bytes())
		bufs.Put(b)
	}
}
