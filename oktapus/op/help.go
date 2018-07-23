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

	"github.com/LuminalHQ/cloudcover/oktapus/internal"
)

// Help writes global or command-specific help information to stderr and exits.
func Help(ci *CmdInfo) {
	w, bin, exit := helpSetup(nil)
	defer exit()
	fmt.Fprintf(w, "%s v%s\n", internal.AppName, internal.AppVersion)
	usage(w, bin, ci)
	if w.WriteByte('\n'); ci == nil {
		globalHelp(w)
	} else {
		cmdHelp(w, ci)
	}
}

// UsageErr reports a command-related error and exits.
func UsageErr(cmd Cmd, err error) {
	w, bin, exit := helpSetup(err)
	defer exit()
	if cmd == nil {
		usage(w, bin, nil)
	} else {
		usage(w, bin, cmd.Info())
	}
}

// UsageErrf reports a problem with the command-line arguments and exits.
func UsageErrf(cmd Cmd, format string, v ...interface{}) {
	UsageErr(cmd, fmt.Errorf(format, v...))
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
		code = 2
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

// usage writes command usage summary to w.
func usage(w *bufio.Writer, bin string, ci *CmdInfo) {
	if ci == nil {
		fmt.Fprintf(w, "Usage: %s command [options] args\n", bin)
		fmt.Fprintf(w, "       %s command help\n", bin)
		fmt.Fprintf(w, "       %s help [command]\n", bin)
		return
	}
	name := ci.Names[0]
	if aliases := ci.Names[1:]; len(aliases) > 0 {
		name = fmt.Sprintf("{%s|%s}", name, strings.Join(aliases, "|"))
	}
	sp, usage := " ", ci.Usage
	if len(usage) == 0 {
		sp = ""
	}
	fmt.Fprintf(w, "Usage: %s %s%s%s\n", bin, name, sp, usage)
	fmt.Fprintf(w, "       %s %s help\n", bin, name)
}

// globalHelp writes global help information to w and exits.
func globalHelp(w *bufio.Writer) {
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
	w.WriteByte('\n')
}

// cmdHelp writes command-specific help information to w and exits.
func cmdHelp(w *bufio.Writer, ci *CmdInfo) {
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
