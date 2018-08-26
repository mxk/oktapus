package cmd

import (
	"errors"
	"io"
	"log"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/LuminalHQ/cloudcover/oktapus/internal"
	"github.com/LuminalHQ/cloudcover/oktapus/op"
	"github.com/LuminalHQ/cloudcover/x/cli"
	"github.com/aws/aws-sdk-go-v2/aws/external"
)

// TODO: Okta mode
// TODO: Parallel execution (close stdin, capture stdout/stderr)?

var execCli = cli.Main.Add(&cli.Info{
	Name:    "exec",
	Usage:   "[options] account-spec command [args ...]",
	Summary: "Run external command for multiple accounts",
	MinArgs: 2,
	New:     func() cli.Cmd { return &execCmd{Dur: 5 * time.Minute} },
})

type execCmd struct {
	Dur    time.Duration `flag:"Minimum credential validity <duration>"`
	Spec   string
	Cmd    string
	Args   []string
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
}

func (*execCmd) Info() *cli.Info { return execCli }

func (*execCmd) Help(w *cli.Writer) {
	w.Text(`
	Run an external command for one or more accounts with temporary security
	credentials.

	The specified command is executed for each account matching account spec.
	Credentials are passed in standard AWS environment variables. The following
	command executes AWS CLI, which reports caller identity for all accessible
	accounts:

	  oktapus exec '' aws sts get-caller-identity
	`)
	accountSpecHelp(w)
}

func (cmd *execCmd) Main(args []string) error {
	cmd.Spec, cmd.Cmd, cmd.Args = args[0], args[1], args[2:]
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	_, err := op.Run(cmd)
	return err
}

func (cmd *execCmd) Run(ctx *op.Ctx) (interface{}, error) {
	path, err := exec.LookPath(cmd.Cmd)
	if err != nil {
		return nil, err
	}
	tpl := exec.Cmd{
		Path:   path,
		Args:   append([]string{cmd.Cmd}, cmd.Args...),
		Env:    execEnv(ctx),
		Stdin:  cmd.Stdin,
		Stdout: cmd.Stdout,
		Stderr: cmd.Stderr,
	}
	acs, err := ctx.Match(cmd.Spec)
	if err != nil {
		return nil, err
	}
	if 0 <= cmd.Dur && cmd.Dur <= 30*time.Minute {
		// Try to refresh all creds at once, but long-running commands with many
		// accounts will require another refresh before each invocation.
		acs.EnsureCreds(cmd.Dur + minDur)
	}
	log.SetPrefix("==> ")
	var credsErr, runErr int
	for _, ac := range acs {
		log.Printf("ACCOUNT %s: %s", ac.ID, ac.Name)
		if err := ac.CredsProvider().Ensure(cmd.Dur); err != nil {
			log.Println("ERROR:", err)
			credsErr++
		} else if err = run(tpl, ac); err != nil {
			log.Println("ERROR:", err)
			if runErr++; runErr == 1 && internal.ExitCode(err) == 2 {
				return nil, errors.New("abort due to command usage error")
			}
		}
	}
	if n := credsErr + runErr; n != 0 {
		log.Printf("TOTAL ERRORS: %d (%d due to invalid credentials)",
			n, credsErr)
		err = cli.ExitCode(1)
	}
	return nil, err
}

func execEnv(ctx *op.Ctx) []string {
	i, env := 0, os.Environ()
	for _, e := range env {
		if !strings.HasPrefix(e, "AWS_") && !strings.HasPrefix(e, "OKTA_") {
			env[i] = e
			i++
		}
	}
	env = append(env[:i], external.AWSDefaultRegionEnvVar+"="+ctx.Cfg().Region)
	if v := ctx.EnvCfg.SharedConfigProfile; v != "" {
		env = append(env, external.AWSProfileEnvVar+"="+v)
	}
	if v := ctx.EnvCfg.SharedConfigFile; v != "" {
		env = append(env, external.AWSConfigFileEnvVar+"="+v)
	}
	if v := ctx.EnvCfg.CustomCABundle; v != "" {
		env = append(env, external.AWSCustomCABundleEnvVar+"="+v)
	}
	return env[:len(env):len(env)]
}

func run(c exec.Cmd, ac *op.Account) error {
	cr, _ := ac.CredsProvider().Creds()
	c.Env = append(c.Env,
		external.AWSAccessKeyIDEnvVar+"="+cr.AccessKeyID,
		external.AWSSecreteAccessKeyEnvVar+"="+cr.SecretAccessKey,
		external.AWSSessionTokenEnvVar+"="+cr.SessionToken,
	)
	return c.Run()
}
