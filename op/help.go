package op

import (
	"bufio"
	"bytes"
	"flag"
	"fmt"
	"io"
	"os"
	"runtime/debug"
	"sort"
	"strings"

	"github.com/LuminalHQ/oktapus/internal"
)

// usageError indicates a problem with the command-line arguments.
type usageError string

func (e usageError) Error() string { return string(e) }

// UsageErr reports a problem with the command-line arguments and exits.
func UsageErr(cmd Cmd, format string, v ...interface{}) {
	if err := usageError(fmt.Sprintf(format, v...)); cmd != nil {
		CmdHelp(cmd.Info(), err)
	} else {
		Help(err)
	}
}

// Help writes global help information and command summary to stderr before
// exiting.
func Help(err error) {
	w, bin, exit := helpSetup(err)
	defer exit()
	fmt.Fprintf(w, "%s v%s\n", internal.AppName, internal.AppVersion)
	fmt.Fprintf(w, "Usage: %s command [options] args\n", bin)
	fmt.Fprintf(w, "       %s command help\n", bin)
	fmt.Fprintf(w, "       %s help [command]\n\n", bin)
	w.WriteString("AWS account management and creation tool.\n\n")
	w.WriteString("Commands:\n")
	names, maxLen := make([]string, 0, len(cmds)), 0
	for name, ci := range cmds {
		if name == ci.Names[0] && !ci.Hidden {
			if names = append(names, name); maxLen < len(name) {
				maxLen = len(name)
			}
		}
	}
	sort.Strings(names)
	for _, name := range names {
		fmt.Fprintf(w, "  %-*s  %s\n", maxLen, name, cmds[name].Summary)
	}
	AccountSpecHelp(w)
	w.WriteByte('\n')
}

// CmdHelp writes command-specific help information to stderr before exiting.
func CmdHelp(ci *CmdInfo, err error) {
	w, bin, exit := helpSetup(err)
	defer exit()
	name := ci.Names[0]
	if aliases := ci.Names[1:]; len(aliases) > 0 {
		name = fmt.Sprintf("{%s|%s}", name, strings.Join(aliases, "|"))
	}
	sp, usage := " ", ci.Usage
	if len(usage) == 0 {
		sp = ""
	}
	fmt.Fprintf(w, "Usage: %s %s%s%s\n", bin, name, sp, usage)
	fmt.Fprintf(w, "       %s %s help\n\n", bin, name)
	cmd := ci.New()
	cmd.Help(w)
	var fs flag.FlagSet
	var buf bytes.Buffer
	cmd.FlagCfg(&fs)
	fs.SetOutput(&buf)
	if fs.PrintDefaults(); buf.Len() > 0 {
		w.WriteString("\nOptions:\n")
		w.Write(buf.Bytes())
	}
	w.WriteByte('\n')
}

func AccountSpecHelp(w *bufio.Writer) {
	w.WriteByte('\n')
	WriteHelp(w, `
		Run 'oktapus help account-spec' for details on account filtering
		specifications.
	`)
}

// WriteHelp writes multi-line string s to w, removing any indentation.
func WriteHelp(w *bufio.Writer, s string) {
	w.WriteString(strings.TrimSpace(internal.Dedent(s)))
	w.WriteByte('\n')
}

// helpArgs contains arguments that trigger help display.
var helpArgs = map[string]struct{}{
	"help": {}, "-help": {}, "--help": {}, "-h": {}, "/?": {},
}

// isHelp returns true if s represents a command-line argument asking for help.
func isHelp(s string) bool {
	_, ok := helpArgs[s]
	return ok
}

var (
	helpWriter   = io.Writer(os.Stderr)
	helpExitFunc = os.Exit
)

// helpSetup writes an error report to stderr and sets program exit code.
func helpSetup(err error) (w *bufio.Writer, bin string, exit func()) {
	w, code := bufio.NewWriter(helpWriter), 0
	if err != nil {
		code = 1
		if _, ok := err.(usageError); ok {
			code = 2
		}
		fmt.Fprintf(w, "Error: %v\n", err)
	}
	return w, internal.AppName, func() {
		defer helpExitFunc(2)
		if p := recover(); p != nil {
			w.WriteString("panic: ")
			fmt.Fprintln(w, p)
			w.WriteByte('\n')
			w.Write(debug.Stack())
			code = 2
		}
		w.Flush()
		helpExitFunc(code)
	}
}
