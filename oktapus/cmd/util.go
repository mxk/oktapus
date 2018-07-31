package cmd

import (
	"bufio"
	"encoding/gob"
	"encoding/json"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/LuminalHQ/cloudcover/oktapus/internal"
	"github.com/LuminalHQ/cloudcover/oktapus/op"
	"github.com/LuminalHQ/cloudcover/x/cli"
	"github.com/LuminalHQ/cloudcover/x/fast"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/awserr"
)

var log = internal.Log

func init() {
	gob.Register([]*credsOutput{})
	gob.Register([]*listOutput{})
	gob.Register([]*newAccountsOutput{})
	gob.Register([]*resultsOutput{})
	gob.Register([]*rmOutput{})
}

// OutFmt implements the -out flag for commands that print table or JSON
// output.
type OutFmt struct {
	Out string `flag:",Output <format> (choice: text, json)"`
}

// Print writes command output to stdout. When text format is used, cfg and fn
// are forwarded to the printer.
func (f OutFmt) Print(v interface{}) error {
	w := bufio.NewWriter(os.Stdout)
	defer w.Flush()
	if f.Out == "" || f.Out == "text" {
		internal.NewPrinter(v).Print(w, nil)
		return nil
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "\t")
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
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
		var cr aws.Credentials
		err := ac.Err
		// Credentials do not require account control information
		if err == nil || err == op.ErrNoCtl {
			cp := ac.CredsProvider()
			d := 10 * time.Minute
			if renew {
				d = -1
			}
			if err = cp.Ensure(d); err != nil {
				ac.Err = err
			}
			cr, _ = cp.Creds()
		}
		co := &credsOutput{
			AccountID: ac.ID,
			Name:      ac.Name,
			Error:     explainError(err),
		}
		if err == nil {
			co.Expires = expTime{cr.Expires}
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
	return t.Sub(fast.Time()).Truncate(time.Second).String()
}

// register registers a new CLI command.
func register(ci *cli.Info) *cli.Info {
	cli.Main.Add(ci)
	cmd := ci.New()
	if _, ok := cmd.(op.CallableCmd); ok {
		gob.Register(cmd)
	}
	return ci
}

// padArgs grows args to cmd's maximum number of arguments.
func padArgs(cmd cli.Cmd, args *[]string) {
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
