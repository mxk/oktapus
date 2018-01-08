package cmd

import (
	"bufio"
	"encoding/json"
	"flag"
	"io/ioutil"
	"net/http"
	"os"
	"strings"
	"syscall"

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
	switch e := err.(type) {
	case awserr.RequestFailure:
		switch e.StatusCode() {
		case http.StatusForbidden:
			return "account access denied"
		case http.StatusNotFound:
			return "account control not initialized"
		default:
			return e.Code() + ": " + e.Message()
		}
	case awserr.Error:
		return e.Code() + ": " + e.Message()
	case error:
		return e.Error()
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

// HaveOpt returns true if the specified flag name was set on the command line.
func (c *command) HaveOpt(name string) bool {
	set := false
	if c.Flags != nil {
		c.Flags.Visit(func(f *flag.Flag) {
			if f.Name == name {
				set = true
			}
		})
	}
	return set
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

// byName implements sort.Interface to sort accounts by name.
type byName []*Account

func (a byName) Len() int           { return len(a) }
func (a byName) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
func (a byName) Less(i, j int) bool { return a[i].Name < a[j].Name }
