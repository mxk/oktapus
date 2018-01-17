package cmd

import (
	"bufio"
	"encoding/json"
	"flag"
	"io/ioutil"
	"net/http"
	"os"
	"reflect"
	"sort"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"github.com/LuminalHQ/oktapus/awsgw"
	"github.com/LuminalHQ/oktapus/internal"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"golang.org/x/crypto/ssh/terminal"
)

// cmds maps command names to their implementations. New commands are added by
// calling register() from an init() function.
var cmds = make(map[string]Cmd)

// Cmd defines the common command interface.
type Cmd interface {
	Name() string      // Canonical command name
	Aliases() []string // Alternate command names
	Summary() string   // Short description for the main help page
	Usage() string     // Syntax of options and arguments
	NArgs() (int, int) // Min/max number of positional arguments
	Hidden() bool      // Hide the command from the main help page

	Help(w *bufio.Writer)              // Writes detailed help info to w
	FlagCfg(fs *flag.FlagSet)          // Configures flags
	Run(ctx *Ctx, args []string) error // Runs command
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
		cmdHelp(cmd, err)
	}

	// Verify positional argument count
	args = fs.Args()
	if min, max := cmd.NArgs(); min == max && len(args) != min {
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
	ctx := NewCtx()
	defer ctx.Save()
	if err := cmd.Run(ctx, args); err != nil {
		log.F("Command error: %v\n", err)
	}
}

// register adds a new command to the cmds map.
func register(cmd Cmd) {
	name := cmd.Name()
	if _, ok := cmds[name]; ok {
		panic("cmd: duplicate command name: " + name)
	}
	cmds[name] = cmd
	for _, alias := range cmd.Aliases() {
		if _, ok := cmds[alias]; ok {
			panic("cmd: duplicate command alias: " + alias)
		}
		cmds[alias] = cmd
	}
}

// getCmd returns the requested command and updated args. If the user requested
// help, it shows the relevant help information and exits.
func getCmd(args []string) (Cmd, []string) {
	if len(args) > 0 {
		if cmd := cmds[args[0]]; cmd != nil {
			if len(args) > 1 && isHelp(args[1]) {
				cmdHelp(cmd, nil)
			}
			return cmd, args[1:]
		}
		var unknown string
		if !isHelp(args[0]) {
			unknown = args[0]
		} else if len(args) > 1 {
			if cmd := cmds[args[1]]; cmd != nil {
				cmdHelp(cmd, nil)
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

// command provides a partial implementation of the Cmd interface.
type command struct {
	name    []string
	summary string
	usage   string
	minArgs int
	maxArgs int
	hidden  bool
	help    string

	Flags  *flag.FlagSet
	OutFmt string // -out flag

	setFlags map[unsafe.Pointer]struct{}
}

func (c *command) Name() string      { return c.name[0] }
func (c *command) Aliases() []string { return c.name[1:] }
func (c *command) Summary() string   { return c.summary }
func (c *command) Usage() string     { return c.usage }
func (c *command) NArgs() (int, int) { return c.minArgs, c.maxArgs }
func (c *command) Hidden() bool      { return c.hidden }

// Help writes detailed command help information to w.
func (c *command) Help(w *bufio.Writer) {
	if c.help != "" {
		w.WriteString(strings.TrimSpace(c.help))
	} else {
		w.WriteString(c.summary)
		w.WriteByte('.')
	}
	w.WriteByte('\n')
}

// FlagCfg configures command flags.
func (c *command) FlagCfg(fs *flag.FlagSet) {
	c.Flags = fs
	out := "json"
	if terminal.IsTerminal(syscall.Stdout) {
		out = "text"
	}
	fs.StringVar(&c.OutFmt, "out", out, "Output `format`: text|json")
}

// PadArgs ensures that args has at least maxArgs values.
func (c *command) PadArgs(args *[]string) {
	if n := len(*args); n < c.maxArgs {
		if cap(*args) >= c.maxArgs {
			*args = (*args)[:c.maxArgs]
		} else {
			tmp := make([]string, c.maxArgs)
			copy(tmp, *args)
			*args = tmp
		}
	}
}

// HaveFlag returns true if the specified flag was set on the command line. Ptr
// must be a pointer obtained from or given to one of flag.FlagSet methods.
func (c *command) HaveFlag(ptr interface{}) bool {
	if c.setFlags == nil {
		if c.Flags == nil || c.Flags.NFlag() == 0 {
			return false
		}
		m := make(map[unsafe.Pointer]struct{}, c.Flags.NFlag())
		c.Flags.Visit(func(f *flag.Flag) {
			p := unsafe.Pointer(reflect.ValueOf(f.Value).Pointer())
			m[p] = struct{}{}
		})
		c.setFlags = m
	}
	_, ok := c.setFlags[unsafe.Pointer(reflect.ValueOf(ptr).Pointer())]
	return ok
}

// PrintOutput writes command output to stdout. When text format is used, cfg
// and fn are forwarded to the printer.
func (c *command) PrintOutput(v interface{}) error {
	w := bufio.NewWriter(os.Stdout)
	defer w.Flush()
	if c.OutFmt == "text" {
		internal.NewPrinter(v).Print(w, nil)
		return nil
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "\t")
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}

// usageError indicates a problem with the command-line arguments.
type usageError string

func (e usageError) Error() string { return string(e) }

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
			co.Expires = expTime(cr.Exp)
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
type expTime time.Time

func (t expTime) MarshalJSON() ([]byte, error) {
	if time.Time(t).IsZero() {
		return []byte(`""`), nil
	}
	return time.Time(t).MarshalJSON()
}

func (t expTime) String() string {
	if time.Time(t).IsZero() {
		return ""
	}
	return time.Time(t).Sub(internal.Time()).Truncate(time.Second).String()
}
