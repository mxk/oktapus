package cmd

import (
	"bufio"
	"encoding/json"
	"net/http"
	"os"
	"time"

	"github.com/LuminalHQ/cloudcover/oktapus/internal"
	"github.com/LuminalHQ/cloudcover/oktapus/op"
	"github.com/LuminalHQ/cloudcover/x/fast"
	"github.com/aws/aws-sdk-go-v2/aws/awserr"
	"github.com/pkg/errors"
)

// get returns v[i] or an empty string if i is out of bounds.
func get(v []string, i int) string {
	if 0 <= i && i < len(v) {
		return v[i]
	}
	return ""
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
		// TODO: Probably no longer needed
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

// OutFmt implements the common -json flag.
type OutFmt struct {
	JSON bool `flag:",Write JSON output"`
}

// Print writes command output to stdout.
func (f OutFmt) Print(v interface{}) error {
	w := bufio.NewWriter(os.Stdout)
	defer w.Flush()
	if f.JSON {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "\t")
		enc.SetEscapeHTML(false)
		return errors.WithStack(enc.Encode(v))
	}
	internal.NewPrinter(v).Print(w, nil)
	return nil
}

// resultsOutput is the result of an account operation that does not provide any
// other output.
type resultsOutput struct {
	Account string
	Name    string
	Result  string
}

func listResults(acs op.Accounts) []*resultsOutput {
	out := make([]*resultsOutput, 0, len(acs))
	for _, ac := range acs {
		result := "OK"
		if ac.Err != nil {
			result = "ERROR: " + explainError(ac.Err)
		}
		out = append(out, &resultsOutput{
			Account: ac.ID,
			Name:    ac.Name,
			Result:  result,
		})
	}
	return out
}

// credsOutput provides account credentials to the user.
type credsOutput struct {
	Account         string
	Name            string
	Expires         expTime
	AccessKeyID     string
	SecretAccessKey string
	SessionToken    string `printer:",width=1,last"`
	Error           string
}

func listCreds(acs op.Accounts, renew bool) []*credsOutput {
	out := make([]*credsOutput, len(acs))
	acs.Map(func(i int, ac *op.Account) error {
		cp := ac.CredsProvider()
		d := 10 * time.Minute
		if renew {
			d = -1
		}
		err := cp.Ensure(d)
		cr, _ := cp.Creds()
		co := &credsOutput{
			Account: ac.ID,
			Name:    ac.Name,
			Error:   explainError(err),
		}
		if err == nil {
			co.Expires = expTime{cr.Expires}
			co.AccessKeyID = cr.AccessKeyID
			co.SecretAccessKey = cr.SecretAccessKey
			co.SessionToken = cr.SessionToken
		}
		out[i] = co
		return nil
	})
	return out
}

func (o *credsOutput) PrintRow(p *internal.Printer) {
	if o.Error == "" {
		internal.PrintRow(p, o)
	} else {
		p.PrintCol(0, o.Account, true)
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
