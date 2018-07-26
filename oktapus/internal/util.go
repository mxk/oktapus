package internal

import (
	"bytes"
	"encoding/json"
	"io"
	"io/ioutil"
	"math/rand"

	"github.com/LuminalHQ/cloudcover/x/fast"
)

func init() {
	rand.Seed(int64(fast.RandUint64()))
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
