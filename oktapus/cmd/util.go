package cmd

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/LuminalHQ/cloudcover/oktapus/internal"
	"github.com/LuminalHQ/cloudcover/oktapus/op"
	"github.com/LuminalHQ/cloudcover/x/arn"
	"github.com/LuminalHQ/cloudcover/x/fast"
	"github.com/LuminalHQ/cloudcover/x/iamx"
	"github.com/aws/aws-sdk-go-v2/aws/awserr"
	"github.com/pkg/errors"
)

// minDur is minimum credential validity duration for internal operations.
const minDur = 2 * time.Minute

// get returns v[i] or an empty string if i is out of bounds.
func get(v []string, i int) string {
	if 0 <= i && i < len(v) {
		return v[i]
	}
	return ""
}

// splitPathName splits a user or role "[path/]name" string into its components.
// If the path is missing, it defaults to op.IAMPath or op.IAMTmpPath, depending
// on tmp. Otherwise, the original path is used, but op.IAMTmpPath prefix is
// forced if tmp is true.
func splitPathName(pathName string, tmp bool) (path, name string, err error) {
	if strings.IndexByte(pathName, ':') != -1 {
		err = fmt.Errorf("invalid path/name %q", pathName)
		return
	}
	r := (arn.Base + "/").WithPathName(pathName)
	if name = r.Name(); strings.IndexByte(pathName, '/') != -1 {
		if path = r.Path(); tmp {
			path = op.IAMTmpPath + path[1:]
		}
	} else if tmp {
		path = op.IAMTmpPath
	} else {
		path = op.IAMPath
	}
	return
}

// getManagedPolicy returns the ARN of the requested managed policy or an error
// if the policy name is invalid.
func getManagedPolicy(partition, policy string) (arn.ARN, error) {
	if policy == "" {
		return "", nil
	} else if p := iamx.ManagedPolicyARN(partition, policy); p != "" {
		return p, nil
	}
	return "", fmt.Errorf("invalid policy name %q", policy)
}

// explainError returns a user-friendly representation of err.
func explainError(err error) string {
	switch err := err.(type) {
	case awserr.RequestFailure:
		if err.StatusCode() == http.StatusForbidden {
			return op.ErrNoAccess.Error()
		}
		return err.Error()
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
	out := make([]*resultsOutput, len(acs))
	for i, ac := range acs {
		result := "OK"
		if ac.Err != nil {
			result = "ERROR: " + explainError(ac.Err)
		}
		out[i] = &resultsOutput{
			Account: ac.ID,
			Name:    ac.Name,
			Result:  result,
		}
	}
	return out
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
