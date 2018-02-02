package cmd

import (
	"bufio"
	"encoding/json"
	"flag"
	"net/http"
	"os"
	"strings"
	"syscall"

	"github.com/LuminalHQ/oktapus/internal"
	"github.com/LuminalHQ/oktapus/op"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"golang.org/x/crypto/ssh/terminal"
)

// Name provides common Cmd method implementations.
type Name string

func (n Name) Info() *op.CmdInfo {
	return op.GetCmdInfo(string(n))
}

func (n Name) Help(w *bufio.Writer) {
	ci := op.GetCmdInfo(string(n))
	w.WriteString(ci.Summary)
	w.WriteString(".\n")
	if strings.Contains(ci.Usage, "account-spec") {
		op.AccountSpecHelp(w)
	}
}

// PrintFmt implements the -out flag for commands that print table or JSON output.
type PrintFmt string

// flag.Value interface.
func (f PrintFmt) String() string { return string(f) }
func (f *PrintFmt) Set(s string) error {
	*f = PrintFmt(s)
	return nil
}

func (f *PrintFmt) FlagCfg(fs *flag.FlagSet) {
	out := "json"
	if terminal.IsTerminal(syscall.Stdout) {
		out = "text"
	}
	*f = PrintFmt(out)
	fs.Var(f, "out", "Output `format`: text|json")
}

// Print writes command output to stdout. When text format is used, cfg and fn
// are forwarded to the printer.
func (f PrintFmt) Print(v interface{}) error {
	w := bufio.NewWriter(os.Stdout)
	defer w.Flush()
	if f == "text" {
		internal.NewPrinter(v).Print(w, nil)
		return nil
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "\t")
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}

// explainError returns a user-friendly representation of err.
func explainError(err error) string {
	switch err := err.(type) {
	case awserr.RequestFailure:
		switch err.StatusCode() {
		case http.StatusForbidden:
			return "account access denied"
		case http.StatusNotFound:
			return "account control not initialized"
		default:
			return err.Code() + ": " + err.Message()
		}
	case awserr.Error:
		if err.Code() == "NoCredentialProviders" {
			errs := err.(awserr.BatchedErrors).OrigErrs()
			if n := len(errs); n > 0 {
				return explainError(errs[n-1])
			}
		}
		return err.Code() + ": " + err.Message()
	case error:
		return err.Error()
	}
	return ""
}
