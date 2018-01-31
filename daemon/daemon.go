package daemon

import (
	"bufio"
	"crypto/sha512"
	"encoding/base64"
	"encoding/gob"
	"os/exec"
	"os/user"

	"github.com/LuminalHQ/oktapus/internal"
)

var log = internal.Log

func init() {
	gob.Register(new(Result))
	gob.Register(new(internal.LogMsg))
}

// Ctx defines hooks for configuring the daemon.
type Ctx interface {
	DaemonSig(w *bufio.Writer)
	DaemonCmd(addr string) *exec.Cmd
}

// Cmd is a command sent to the daemon for execution.
type Cmd struct {
	Cmd  interface{}
	Args []string
	Out  chan<- *Result
}

// Result is the command execution result.
type Result struct {
	Out interface{}
	Err error
}

// sig returns an environment signature, which is a hash of variables that
// affect daemon behavior.
func sig(ctx Ctx) string {
	h := sha512.New()
	w := bufio.NewWriter(h)
	uid := ""
	if u, err := user.Current(); err == nil {
		uid = u.Uid
	}
	lines := [...][2]string{
		{"VERSION", internal.AppVersion},
		{"UID", uid},
	}
	for _, s := range lines {
		w.WriteString(s[0])
		w.WriteByte('=')
		w.WriteString(s[1])
		w.WriteByte('\n')
	}
	ctx.DaemonSig(w)
	var s [sha512.Size]byte
	w.Flush()
	h.Sum(s[:0])
	return base64.URLEncoding.EncodeToString(s[:12])
}
