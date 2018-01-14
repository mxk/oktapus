package internal

import (
	"bytes"
	"encoding/json"
	"io"
	"io/ioutil"
	"sync/atomic"
	"time"
)

var now atomic.Value

func init() {
	now.Store(time.Now())
	go func() {
		t := time.Now()
		d := t.Truncate(time.Second).Add(time.Second).Sub(t)
		if d < 250*time.Millisecond {
			d += time.Second
		}
		now.Store(<-time.After(d))
		for t := range time.Tick(time.Second) {
			now.Store(t)
		}
	}()
}

// Time returns the current time. It is much faster than time.Now(), but has a
// resolution of 1 second.
func Time() time.Time {
	return now.Load().(time.Time)
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
