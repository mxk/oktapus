package cmd

import (
	"bufio"
	"encoding/gob"
	"encoding/json"
	"flag"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/LuminalHQ/oktapus/awsgw"
	"github.com/LuminalHQ/oktapus/internal"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"golang.org/x/crypto/ssh/terminal"
)

// cmds maps command names to info structs. New commands are added by calling
// register() from init().
var cmds = make(map[string]*cmdInfo)

// cmdInfo contains basic command information.
type cmdInfo struct {
	names   []string
	summary string
	usage   string
	minArgs int
	maxArgs int
	hidden  bool
	new     func() Cmd
}

// Cmd is an executable command interface.
type Cmd interface {
	Info() *cmdInfo                    // Get command information
	Help(w *bufio.Writer)              // Write detailed help info to w
	FlagCfg(fs *flag.FlagSet)          // Configure flags
	Run(ctx *Ctx, args []string) error // Run command
}

// CallableCmd is a command that can be called remotely.
type CallableCmd interface {
	Cmd
	Call(ctx *Ctx) (interface{}, error)
}

// Run is the main program entry point. It executes the command specified by
// args.
func Run(args []string) {
	// Get command and parse options
	cmd, args := getCmd(args)
	fs := flag.FlagSet{Usage: func() {}}
	fs.SetOutput(ioutil.Discard)
	cmd.FlagCfg(&fs)
	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			err = nil
		}
		cmdHelp(cmd.Info(), err)
	}

	// Verify positional argument count
	args, ci := fs.Args(), cmd.Info()
	if min, max := ci.minArgs, ci.maxArgs; min == max && len(args) != min {
		if min <= 0 {
			usageErr(cmd, "command does not accept any arguments")
		} else {
			usageErr(cmd, "command requires %d argument(s)", min)
		}
	} else if len(args) < min {
		usageErr(cmd, "command requires at least %d argument(s)", min)
	} else if min < max && max < len(args) {
		usageErr(cmd, "command accepts at most %d argument(s)", max)
	}

	// Run
	if err := cmd.Run(NewCtx(), args); err != nil {
		log.F("Command error: %v\n", err)
	}
}

// register adds new command information to the cmds map.
func register(ci *cmdInfo) {
	for _, name := range ci.names {
		if _, ok := cmds[name]; ok {
			panic("duplicate command name: " + name)
		}
		cmds[name] = ci
	}
	cmd := ci.new()
	if _, ok := cmd.(CallableCmd); ok {
		gob.Register(cmd)
	}
}

// getCmd returns the requested command and updated args. If the user requested
// help, it shows the relevant help information and exits.
func getCmd(args []string) (Cmd, []string) {
	if len(args) > 0 {
		if ci := cmds[args[0]]; ci != nil {
			if len(args) > 1 && isHelp(args[1]) {
				cmdHelp(ci, nil)
			}
			return ci.new(), args[1:]
		}
		var unknown string
		if !isHelp(args[0]) {
			unknown = args[0]
		} else if len(args) > 1 {
			if ci := cmds[args[1]]; ci != nil {
				cmdHelp(ci, nil)
			}
			unknown = args[1]
		}
		if unknown != "" {
			usageErr(nil, "unknown command %q", unknown)
		}
	}
	help(nil)
	panic("never reached")
}

// padArgs grows args to cmd's maximum number of arguments.
func padArgs(cmd Cmd, args *[]string) {
	max := cmd.Info().maxArgs
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

// Name provides common Cmd method implementations.
type Name string

func (n Name) Info() *cmdInfo {
	return cmds[string(n)]
}

func (n Name) Help(w *bufio.Writer) {
	ci := cmds[string(n)]
	w.WriteString(ci.summary)
	w.WriteString(".\n")
	if strings.Contains(ci.usage, "account-spec") {
		accountSpecHelp(w)
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

// strPtrValue implements flag.Value for *string types.
type strPtrValue struct{ v **string }

func StringPtrVar(fs *flag.FlagSet, p **string, name string, usage string) {
	fs.Var(strPtrValue{p}, name, usage)
}

func (s strPtrValue) String() string {
	if s.v == nil || *s.v == nil {
		return ""
	}
	return **s.v
}

func (s strPtrValue) Set(val string) error {
	*s.v = &val
	return nil
}

// boolPtrValue implements flag.Value for *bool types.
type boolPtrValue struct{ v **bool }

func BoolPtrVar(fs *flag.FlagSet, p **bool, name string, usage string) {
	fs.Var(boolPtrValue{p}, name, usage)
}

func (b boolPtrValue) String() string {
	if b.v == nil || *b.v == nil {
		return "false"
	}
	return strconv.FormatBool(**b.v)
}

func (b boolPtrValue) Set(val string) error {
	v, err := strconv.ParseBool(val)
	*b.v = &v
	return err
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

func listResults(acs Accounts) []*resultsOutput {
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

func listCreds(acs Accounts, renew bool) []*credsOutput {
	out := make([]*credsOutput, 0, len(acs))
	for _, ac := range acs {
		var cr *awsgw.StaticCreds
		if ac.Err == nil {
			cr, ac.Err = ac.Creds(renew)
		}
		co := &credsOutput{
			AccountID: ac.ID,
			Name:      ac.Name,
			Error:     explainError(ac.Err),
		}
		if ac.Err == nil {
			co.Expires = expTime{cr.Exp}
			co.AccessKeyID = cr.AccessKeyID
			co.SecretAccessKey = cr.SecretAccessKey
			co.SessionToken = cr.SessionToken
		}
		out = append(out, co)
	}
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

func listAccounts(acs Accounts) []*listOutput {
	out := make([]*listOutput, 0, len(acs))
	for _, ac := range acs {
		if ac.Err == nil && ac.Ctl == nil {
			ac.Err = errNoCtl
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
	return t.MarshalJSON()
}

func (t expTime) String() string {
	if t.IsZero() {
		return ""
	}
	return t.Sub(internal.Time()).Truncate(time.Second).String()
}
