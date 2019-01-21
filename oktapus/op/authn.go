package op

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/mxk/cloudcover/oktapus/okta"
	"golang.org/x/crypto/ssh/terminal"
)

// termAuthn uses the terminal for Okta authentication.
type termAuthn struct {
	user string
	r    io.Reader
	w    io.Writer
}

func newTermAuthn(user string) *termAuthn {
	return &termAuthn{user: user, r: os.Stdin, w: os.Stderr}
}

// Username prompts the user for their Okta username.
func (t *termAuthn) Username() (string, error) {
	for t.user == "" {
		t.println("Okta username: ")
		user, err := readLine(t.r)
		if err != nil {
			return "", err
		}
		t.user = strings.TrimSpace(user)
	}
	return t.user, nil
}

// Password prompts the user for their Okta password.
func (t *termAuthn) Password() (string, error) {
	t.printf("Okta password for %q: ", t.user)
	return t.readSecure("PASSWORD")
}

// Select asks the user to choose one of the options from a menu.
func (t *termAuthn) Select(all []okta.Choice) (okta.Choice, error) {
	prompt := "Your choice? "
	switch c := all[0].(type) {
	case *okta.Factor:
		t.println("Multi-factor authentication required")
		if len(all) == 1 {
			t.println("Using", c.Value())
			return c, nil
		}
		prompt = "Which MFA method do you want to use? "
	}
	for {
		t.println()
		for i, c := range all {
			t.printf("%d) %s\n", i+1, c.Value())
		}
		t.print("\n", prompt)
		ln, err := readLine(t.r)
		if err != nil {
			return nil, err
		}
		if n, err := strconv.Atoi(ln); err == nil && 1 <= n && n <= len(all) {
			return all[n-1], nil
		}
		t.println("Invalid choice, try again")
	}
}

// Input asks the user to respond to an MFA challenge.
func (t *termAuthn) Input(c okta.Choice) (string, error) {
	if p := c.Prompt(); strings.HasSuffix(p, "?") {
		t.print(p, " ")
	} else {
		t.print(p, ": ")
	}
	if f, ok := c.(*okta.Factor); ok && f.FactorType == "question" {
		return t.readSecure("ANSWER")
	}
	return readLine(t.r)
}

// Notify informs the user of MFA status.
func (t *termAuthn) Notify(format string, a ...interface{}) {
	t.printf(format, a...)
}

// readSecure attempts to read sensitive information without terminal echo.
func (t *termAuthn) readSecure(what string) (string, error) {
	if f, ok := t.r.(*os.File); ok {
		if fd := int(f.Fd()); terminal.IsTerminal(fd) {
			pw, err := terminal.ReadPassword(fd)
			t.println()
			return string(pw), err
		}
	}
	t.printf("<WARNING! %s WILL ECHO!> ", what)
	return readLine(t.r)
}

func (t *termAuthn) print(a ...interface{}) {
	fmt.Fprint(t.w, a...)
}

func (t *termAuthn) println(a ...interface{}) {
	fmt.Fprintln(t.w, a...)
}

func (t *termAuthn) printf(format string, a ...interface{}) {
	fmt.Fprintf(t.w, format, a...)
}

// readLine reads a complete line from r without reading past the new line
// character.
func readLine(r io.Reader) (string, error) {
	var ln [256]byte
	for n := 0; n < len(ln); {
		i, err := r.Read(ln[n : n+1])
		if i > 0 {
			if ln[n] == '\n' {
				if n > 0 && ln[n-1] == '\r' {
					n--
				}
				return string(ln[:n]), nil
			}
			n++
		}
		if err != nil {
			if err == io.EOF && n > 0 {
				err = nil
			}
			return string(ln[:n]), err
		}
	}
	return string(ln[:]), errors.New("line too long")
}
