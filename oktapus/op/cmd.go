package op

import (
	"bufio"
	"encoding/gob"
	"flag"
	"io/ioutil"
	"sort"
)

// cmds maps command names to info structs. New commands are added by calling
// Register() from init().
var cmds = make(map[string]*CmdInfo)

// CmdInfo contains basic command information.
type CmdInfo struct {
	Names   []string
	Summary string
	Usage   string
	MinArgs int
	MaxArgs int
	Hidden  bool
	New     func() Cmd
}

// GetCmdInfo returns command information for the given command name.
func GetCmdInfo(name string) *CmdInfo {
	return cmds[name]
}

// CmdNames returns the canonical names of all registered commands.
func CmdNames() []string {
	names := make([]string, 0, len(cmds))
	for _, c := range cmds {
		names = append(names, c.Names[0])
	}
	sort.Strings(names)
	return names
}

// Cmd is an executable command interface.
type Cmd interface {
	Info() *CmdInfo                    // Get command information
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
			Help(cmd.Info())
		}
		UsageErr(cmd, err)
	}

	// Verify positional argument count
	args, ci := fs.Args(), cmd.Info()
	if min, max := ci.MinArgs, ci.MaxArgs; min == max && len(args) != min {
		if min <= 0 {
			UsageErrf(cmd, "command does not accept any arguments")
		} else {
			UsageErrf(cmd, "command requires %d argument(s)", min)
		}
	} else if len(args) < min {
		UsageErrf(cmd, "command requires at least %d argument(s)", min)
	} else if min < max && max < len(args) {
		UsageErrf(cmd, "command accepts at most %d argument(s)", max)
	}

	// Run
	if err := cmd.Run(NewCtx(), args); err != nil {
		log.F("Command failed: %v\n", err)
	}
}

// Register adds new command information to the cmds map.
func Register(ci *CmdInfo) {
	for _, name := range ci.Names {
		if _, ok := cmds[name]; ok {
			panic("duplicate command name: " + name)
		}
		cmds[name] = ci
	}
	cmd := ci.New()
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
				Help(ci)
			}
			return ci.New(), args[1:]
		}
		var unknown string
		if !isHelp(args[0]) {
			unknown = args[0]
		} else if len(args) > 1 {
			if ci := cmds[args[1]]; ci != nil {
				Help(ci)
			}
			unknown = args[1]
		}
		if unknown != "" {
			UsageErrf(nil, "unknown command %q", unknown)
		}
	}
	Help(nil)
	panic("never reached")
}

// strPtrValue implements flag.Value for *string types.
type strPtrValue struct{ v **string }

// StringPtrVar defines a *string flag with specified name and usage string.
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
