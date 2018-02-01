package daemon

import (
	"bufio"
	"crypto/sha512"
	"encoding/base64"
	"encoding/gob"
	"os/exec"
	"sort"

	"github.com/LuminalHQ/oktapus/internal"
)

var log = internal.Log

func init() {
	gob.Register(new(Response))
	gob.Register(new(internal.LogMsg))
}

// Ctx is the interface for starting and locating the daemon process.
type Ctx interface {
	EnvMap() map[string]string
	StartDaemon(c *exec.Cmd) error
}

// Request is a command sent to the daemon for execution.
type Request struct {
	Cmd interface{}
	Out chan<- *Response
}

// Response is the command execution result.
type Response struct {
	Out interface{}
	Err error
}

// sig returns an environment signature, which is a hash of variables that
// affect daemon behavior.
func sig(m map[string]string) string {
	h := sha512.New()
	w := bufio.NewWriter(h)
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		w.WriteString(k)
		w.WriteByte('=')
		w.WriteString(m[k])
		w.WriteByte('\n')
	}
	var s [sha512.Size]byte
	w.Flush()
	h.Sum(s[:0])
	return base64.URLEncoding.EncodeToString(s[:12])
}
