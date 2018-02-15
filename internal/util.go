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
// skipped, the next line containing non-tab characters determines the number of
// tabs to remove.
func Dedent(s string) string {
	n, i := 0, strings.IndexByte(s, '\n')
	for j := i + 1; j < len(s); j++ {
		if c := s[j]; c == '\n' {
			i = j
		} else if c != '\t' {
			n = j - i - 1
			break
		}
	}
	if i == -1 || n == 0 {
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

// GoForEach executes n tasks using at most batch goroutines. If batch is <= 0,
// 64 goroutines are used. Function fn is called for each task with i in the
// range [0,n). If fn returns an error, all pending tasks are canceled and the
// error is returned. It is undefined which error is returned if multiple
// concurrently running tasks return an error.
func GoForEach(n, batch int, fn func(i int) error) error {
	if n <= 1 || batch == 1 {
		for i := 0; i < n; i++ {
			if err := fn(i); err != nil {
				return err
			}
		}
		return nil
	}

	// Adjust batch, ich is needed only when batch < n
	var ich chan int
	if batch <= 0 {
		if n < 96 {
			batch = n
		} else {
			batch = 64
			ich = make(chan int)
		}
	} else if batch < n {
		ich = make(chan int)
	} else {
		batch = n
	}

	// Start waiter goroutine
	ech := make(chan error)
	var wg sync.WaitGroup
	wg.Add(batch)
	go func() {
		defer close(ech)
		wg.Wait()
	}()

	// Start batch goroutines
	for i := 0; i < batch; i++ {
		go func(i int) {
			defer wg.Done()
			if err := fn(i); err != nil {
				ech <- err
			} else if ich != nil {
				for i = range ich {
					if err = fn(i); err != nil {
						ech <- err
						break
					}
				}
			}
		}(i)
	}

	// Send any remaining tasks while waiting for error
	var err error
	if ich != nil {
		for i := batch; i < n; i++ {
			select {
			case ich <- i:
			case err = <-ech:
				i = n - 1
			}
		}
		close(ich)
	}

	// Wait for completion of all tasks
	for err = range ech {
	}
	return err
}
