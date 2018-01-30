package cmd

import (
	"bufio"
	"bytes"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/LuminalHQ/oktapus/internal"
)

// helpArgs contains arguments that trigger help display.
var helpArgs = map[string]struct{}{
	"help": {}, "-help": {}, "--help": {}, "-h": {}, "/?": {},
}

// isHelp returns true if s represents a command-line argument asking for help.
func isHelp(s string) bool {
	_, ok := helpArgs[s]
	return ok
}

// usageErr reports a problem with the command-line arguments and exits.
func usageErr(cmd Cmd, format string, v ...interface{}) {
	if err := usageError(fmt.Sprintf(format, v...)); cmd != nil {
		cmdHelp(cmd, err)
	} else {
		help(err)
	}
}

// help writes global help information and command summary to stderr before
// exiting.
func help(err error) {
	w, bin, exit := helpSetup(err)
	defer exit()
	fmt.Fprintf(w, "%s v%s\n", internal.AppName, internal.AppVersion)
	fmt.Fprintf(w, "Usage: %s command [options] args\n", bin)
	fmt.Fprintf(w, "       %s command help\n", bin)
	fmt.Fprintf(w, "       %s help [command]\n\n", bin)
	w.WriteString("AWS account management and creation tool.\n\n")
	w.WriteString("Commands:\n")
	names, maxLen := make([]string, 0, len(cmds)), 0
	for name, cmd := range cmds {
		if name == cmd.Name() && !cmd.Hidden() {
			if names = append(names, name); maxLen < len(name) {
				maxLen = len(name)
			}
		}
	}
	sort.Strings(names)
	for _, name := range names {
		fmt.Fprintf(w, "    %-*s  %s\n", maxLen, name, cmds[name].Summary())
	}
	accountSpecHelp(w)
	w.WriteByte('\n')
}

// cmdHelp writes command-specific help information to stderr before exiting.
func cmdHelp(cmd Cmd, err error) {
	w, bin, exit := helpSetup(err)
	defer exit()
	name := cmd.Name()
	if aliases := cmd.Aliases(); len(aliases) > 0 {
		name = fmt.Sprintf("{%s|%s}", name, strings.Join(aliases, "|"))
	}
	sp, usage := " ", cmd.Usage()
	if len(usage) == 0 {
		sp = ""
	}
	fmt.Fprintf(w, "Usage: %s %s%s%s\n", bin, name, sp, usage)
	fmt.Fprintf(w, "       %s %s help\n\n", bin, name)
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

// helpSetup writes an error report to stderr and sets program exit code.
func helpSetup(err error) (w *bufio.Writer, bin string, exit func()) {
	w, code := bufio.NewWriter(os.Stderr), 0
	if err != nil {
		code = 1
		if _, ok := err.(usageError); ok {
			code = 2
		}
		fmt.Fprintf(w, "Error: %v\n", err)
	}
	return w, internal.AppName, func() {
		w.Flush()
		os.Exit(code)
	}
}

// accountSpecHelp writes short account-spec help to w.
func accountSpecHelp(w *bufio.Writer) {
	cmds["account-spec"].(*AccountSpec).short(w)
}

// writeHelp writes multi-line string s to w, removing any indentation.
func writeHelp(w *bufio.Writer, s string) {
	w.WriteString(strings.TrimSpace(internal.Dedent(s)))
	w.WriteByte('\n')
}
