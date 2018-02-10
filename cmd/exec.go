package cmd

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"

	"github.com/LuminalHQ/oktapus/internal"
	"github.com/LuminalHQ/oktapus/op"
)

func init() {
	op.Register(&op.CmdInfo{
		Names:   []string{"exec"},
		Summary: "Run external command for multiple accounts",
		Usage:   "account-spec command ...",
		MinArgs: 2,
		New:     func() op.Cmd { return &execCmd{Name: "exec"} },
	})
}

type execCmd struct {
	Name
	noFlags
}

func (cmd *execCmd) Help(w *bufio.Writer) {
	op.WriteHelp(w, `
		Run external command for one or more accounts using temporary security
		credentials.

		The specified command is executed for each account matched by
		account-spec. Account information and credentials are passed in
		environment variables. The following command executes AWS CLI, which
		reports caller identity information for all accessible accounts:

		  oktapus exec '' aws sts get-caller-identity

		The following environment variables are set for each command execution:

		  AWS_ACCOUNT_ID
		  AWS_ACCOUNT_NAME
		  AWS_ACCESS_KEY_ID
		  AWS_SECRET_ACCESS_KEY
		  AWS_SESSION_TOKEN

		Account ID and name are non-standard variables that provide information
		about the current account. The remaining variables are the same ones
		used by AWS CLI and SDKs.
	`)
	op.AccountSpecHelp(w)
}

func (cmd *execCmd) Run(ctx *op.Ctx, args []string) error {
	path, err := exec.LookPath(args[1])
	if err != nil {
		return err
	}
	creds := op.GetCmdInfo("creds").New().(*creds)
	creds.Spec, args = args[0], args[1:]
	out, err := ctx.Call(creds)
	if err != nil {
		return err
	}
	env := os.Environ()
	env = append(make([]string, 0, len(env)+5), env...)
	credsFail, cmdFail := 0, 0
	for _, cr := range out.([]*credsOutput) {
		fmt.Fprintf(os.Stderr, "===> Account %s (%s)\n", cr.AccountID, cr.Name)
		if cr.Error != "" {
			fmt.Fprintf(os.Stderr, "===> ERROR: %s\n", cr.Error)
			credsFail++
			continue
		}
		awsEnv := []string{
			"AWS_ACCOUNT_ID=" + cr.AccountID,
			"AWS_ACCOUNT_NAME=" + cr.Name,
			"AWS_ACCESS_KEY_ID=" + cr.AccessKeyID,
			"AWS_SECRET_ACCESS_KEY=" + cr.SecretAccessKey,
			"AWS_SESSION_TOKEN=" + cr.SessionToken,
		}
		c := exec.Cmd{
			Path:   path,
			Args:   args,
			Env:    append(env, awsEnv...),
			Stdin:  os.Stdin,
			Stdout: os.Stdout,
			Stderr: os.Stderr,
		}
		if err := c.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "===> ERROR: %v\n", err)
			if cmdFail++; cmdFail == 1 && internal.ExitCode(err) == 2 {
				return fmt.Errorf("abort due to command usage error")
			}
		}
	}
	if n := credsFail + cmdFail; n != 0 {
		s := ""
		if n > 1 {
			s = "s"
		}
		return fmt.Errorf("%d command%s failed (%d due to invalid credentials)",
			n, s, credsFail)
	}
	return nil
}
