package cmd

import (
	"bufio"
	"encoding/gob"
	"encoding/json"
	"flag"
	"net/http"
	"os"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/LuminalHQ/cloudcover/oktapus/awsx"
	"github.com/LuminalHQ/cloudcover/oktapus/internal"
	"github.com/LuminalHQ/cloudcover/oktapus/op"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"golang.org/x/crypto/ssh/terminal"
)

var log = internal.Log

// Name provides common Cmd method implementations.
type Name string

// Info returns command information.
func (n Name) Info() *op.CmdInfo {
	return op.GetCmdInfo(string(n))
}

// Help provides a default help implementation based on command summary.
func (n Name) Help(w *bufio.Writer) {
	ci := op.GetCmdInfo(string(n))
	w.WriteString(ci.Summary)
	w.WriteString(".\n")
	if strings.Contains(ci.Usage, "account-spec") {
		accountSpecHelp(w)
	}
}

// noFlags provides a no-op FlagCfg method.
type noFlags struct{}

func (noFlags) FlagCfg(fs *flag.FlagSet) {}

// PrintFmt implements the -out flag for commands that print table or JSON
// output.
type PrintFmt string

// String implements flag.Value.String method.
func (f PrintFmt) String() string { return string(f) }

// Set implements flag.Value.Set method.
func (f *PrintFmt) Set(s string) error {
	*f = PrintFmt(s)
	return nil
}

// FlagCfg adds -out flag to fs.
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

func init() {
	gob.Register([]*resultsOutput{})
	gob.Register([]*credsOutput{})
	gob.Register([]*listOutput{})
}

// resultsOutput is the result of an account operation that does not provide any
// other output.
type resultsOutput struct {
	AccountID string
	Name      string
	Result    string
}

func listResults(acs op.Accounts) []*resultsOutput {
	out := make([]*resultsOutput, 0, len(acs))
	for _, ac := range acs {
		result := "OK"
		if ac.Err != nil {
			result = "ERROR: " + explainError(ac.Err)
		}
		out = append(out, &resultsOutput{
			AccountID: ac.ID,
			Name:      ac.Name,
			Result:    result,
		})
	}
	return out
}

// credsOutput provides account credentials to the user.
type credsOutput struct {
	AccountID       string
	Name            string
	Expires         expTime
	AccessKeyID     string
	SecretAccessKey string
	SessionToken    string `printer:",width=1,last"`
	Error           string
}

func listCreds(acs op.Accounts, renew bool) []*credsOutput {
	out := make([]*credsOutput, len(acs))
	acs.Apply(func(i int, ac *op.Account) {
		var cr *awsx.StaticCreds
		err := ac.Err
		// Credentials do not require account control information
		if err == nil || err == op.ErrNoCtl {
			if cr, err = ac.Creds(renew); err != nil {
				ac.Err = err
			}
		}
		co := &credsOutput{
			AccountID: ac.ID,
			Name:      ac.Name,
			Error:     explainError(err),
		}
		if err == nil {
			co.Expires = expTime{cr.Exp}
			co.AccessKeyID = cr.AccessKeyID
			co.SecretAccessKey = cr.SecretAccessKey
			co.SessionToken = cr.SessionToken
		}
		out[i] = co
	})
	return out
}

func (o *credsOutput) PrintRow(p *internal.Printer) {
	if o.Error == "" {
		internal.PrintRow(p, o)
	} else {
		p.PrintCol(0, o.AccountID, true)
		p.PrintCol(1, o.Name, true)
		p.PrintErr(o.Error)
	}
}

// listOutput provides account information to the user.
type listOutput struct {
	AccountID   string
	Name        string
	Owner       string
	Description string
	Tags        string `printer:",last"`
	Error       string
}

func listAccounts(acs op.Accounts) []*listOutput {
	out := make([]*listOutput, 0, len(acs))
	for _, ac := range acs {
		if ac.Err == nil && ac.Ctl == nil {
			ac.Err = op.ErrNoCtl
		}
		lo := &listOutput{
			AccountID: ac.ID,
			Name:      ac.Name,
			Error:     explainError(ac.Err),
		}
		if ac.Ctl != nil {
			sort.Strings(ac.Tags)
			lo.Owner = ac.Owner
			lo.Description = ac.Desc
			lo.Tags = strings.Join(ac.Tags, ",")
		}
		out = append(out, lo)
	}
	return out
}

func (o *listOutput) PrintRow(p *internal.Printer) {
	if o.Error == "" {
		internal.PrintRow(p, o)
	} else {
		p.PrintCol(0, o.AccountID, true)
		p.PrintCol(1, o.Name, true)
		p.PrintErr(o.Error)
	}
}

// expTime handles credential expiration time encoding for JSON and printer
// outputs.
type expTime struct{ time.Time }

func (t expTime) MarshalJSON() ([]byte, error) {
	if t.IsZero() {
		return []byte(`""`), nil
	}
	return t.Time.MarshalJSON()
}

func (t expTime) String() string {
	if t.IsZero() {
		return ""
	}
	return t.Sub(internal.Time()).Truncate(time.Second).String()
}

// padArgs grows args to cmd's maximum number of arguments.
func padArgs(cmd op.Cmd, args *[]string) {
	max := cmd.Info().MaxArgs
	if n := len(*args); n < max {
		if cap(*args) >= max {
			*args = (*args)[:max]
		} else {
			tmp := make([]string, max)
			copy(tmp, *args)
			*args = tmp
		}
	}
}

// explainError returns a user-friendly representation of err.
func explainError(err error) string {
	switch err := err.(type) {
	case awserr.RequestFailure:
		if err.StatusCode() == http.StatusForbidden {
			return "account access denied"
		}
		return err.Code() + ": " + err.Message()
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
