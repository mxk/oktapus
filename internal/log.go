package internal

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"sync"
)

// Log is the default logger.
var Log = NewLogger(os.Stderr, nil)

// LogFunc is called for each log message. It will never be called concurrently
// by any loggers.
type LogFunc func(m *LogMsg)

// ExitFunc is called by Log.F to terminate the process.
type ExitFunc func(code int)

// LogMsg is passed to LogFunc for each log call.
type LogMsg struct {
	Level byte
	Msg   string
}

// Logger writes messages to an io.Writer and/or a LogFunc.
type Logger struct {
	w io.Writer
	f LogFunc
	e ExitFunc
}

// NewLogger returns a new Logger instance.
func NewLogger(w io.Writer, f LogFunc) *Logger {
	return &Logger{w, f, os.Exit}
}

// logMu is held during all output operations.
var logMu sync.Mutex

// SetWriter sets log output writer. If w is nil, logging to a writer is
// disabled.
func (l *Logger) SetWriter(w io.Writer) (prev io.Writer) {
	logMu.Lock()
	defer logMu.Unlock()
	prev, l.w = l.w, w
	return prev
}

// SetFunc sets log output function. If fn is nil, logging to a function is
// disabled.
func (l *Logger) SetFunc(fn LogFunc) (prev LogFunc) {
	logMu.Lock()
	defer logMu.Unlock()
	prev, l.f = l.f, fn
	return prev
}

// SetExitFunc sets termination function for Log.F.
func (l *Logger) SetExitFunc(fn ExitFunc) (prev ExitFunc) {
	logMu.Lock()
	defer logMu.Unlock()
	prev, l.e = l.e, fn
	return prev
}

// D logs a debug message.
func (l *Logger) D(format string, v ...interface{}) { l.out('D', format, v...) }

// I logs an informational message.
func (l *Logger) I(format string, v ...interface{}) { l.out('I', format, v...) }

// W logs a warning message.
func (l *Logger) W(format string, v ...interface{}) { l.out('W', format, v...) }

// E logs an error message.
func (l *Logger) E(format string, v ...interface{}) { l.out('E', format, v...) }

// F logs an error message and terminates the running process.
func (l *Logger) F(format string, v ...interface{}) {
	l.out('F', format, v...)
	l.exit()
}

// Msg logs message m.
func (l *Logger) Msg(m *LogMsg) { l.out(m.Level, "%s", m.Msg) }

var bufs = sync.Pool{New: func() interface{} {
	return new(bytes.Buffer)
}}

func (l *Logger) out(lvl byte, format string, v ...interface{}) {
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
	logMu.Lock()
	defer logMu.Unlock()
	if l.f != nil {
		msg := b.Bytes()[si:]
		if i := len(msg) - 1; i >= 0 && msg[i] == '\n' {
			msg = msg[:i]
		}
		l.f(&LogMsg{Level: lvl, Msg: string(msg)})
	}
	if l.w != nil {
		l.w.Write(b.Bytes())
		bufs.Put(b)
	}
}

func (l *Logger) exit() {
	if l.e == nil {
		os.Exit(1)
	}
	l.e(1)
	panic("ExitFunc did not terminate execution")
}
