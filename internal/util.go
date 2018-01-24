package internal

import (
	"bytes"
	crand "crypto/rand"
	"encoding/binary"
	"encoding/json"
	"io"
	"io/ioutil"
	"math/rand"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

var (
	timeMu sync.Mutex
	now    atomic.Value
	stop   bool
)

func init() {
	store := func(t time.Time) {
		timeMu.Lock()
		defer timeMu.Unlock()
		if !stop {
			now.Store(t)
		}
	}
	t := time.Now()
	store(t)
	go func() {
		t := time.Now()
		d := t.Truncate(time.Second).Add(time.Second).Sub(t)
		if d < 250*time.Millisecond {
			d += time.Second
		}
		store(<-time.After(d))
		for t := range time.Tick(time.Second) {
			store(t)
		}
	}()
	var b [8]byte
	if _, err := crand.Read(b[:]); err != nil {
		panic(err)
	}
	rand.Seed(int64(binary.LittleEndian.Uint64(b[:])) ^ t.UnixNano())
}

// Time returns the current time. It is much faster than time.Now(), but has a
// resolution of 1 second.
func Time() time.Time {
	return now.Load().(time.Time)
}

// SetTime causes all subsequent Time() calls to return t. If t is the zero
// time, the clock is restarted. This is only used for testing.
func SetTime(t time.Time) {
	zero := t.IsZero()
	timeMu.Lock()
	defer timeMu.Unlock()
	if stop = !zero; zero {
		t = time.Now()
	}
	now.Store(t)
}

// CloseBody attempts to drain http.Response body before closing it to allow
// connection reuse (see https://github.com/google/go-github/pull/317).
func CloseBody(body io.ReadCloser) {
	io.CopyN(ioutil.Discard, body, 4096)
	body.Close()
}

// JSON returns a pretty representation of v.
func JSON(v interface{}) string {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		panic(err)
	}
	return buf.String()
}

// StringsEq returns true if string slices a and b contain identical contents.
func StringsEq(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// Dedent removes leading tab characters from each line in s. The first line is
// ignored, the next non-blank line determines the amount of indentation to
// remove.
func Dedent(s string) string {
	i, n := strings.IndexByte(s, '\n'), 1
	if i != -1 {
	loop:
		for i+n < len(s) {
			switch s[i+n] {
			case '\t':
				n++
			case '\n':
				i, n = i+n, 1
			default:
				break loop
			}
		}
	}
	if n--; n == 0 {
		return s
	}
	b := make([]byte, 0, len(s))
	for i != -1 {
		b, s, i = append(b, s[:i+1]...), s[i+1:], 0
		for i < n && i < len(s) && s[i] == '\t' {
			i++
		}
		s = s[i:]
		i = strings.IndexByte(s, '\n')
	}
	return string(append(b, s...))
}
