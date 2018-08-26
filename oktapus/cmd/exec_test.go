package cmd

import (
	"bytes"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"
	"testing"

	"github.com/LuminalHQ/cloudcover/oktapus/mock"
	"github.com/LuminalHQ/cloudcover/x/cli"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testEnv = "OKTAPUS_TEST_EXEC"

func TestExec(t *testing.T) {
	if os.Getenv(testEnv) != "" {
		env := os.Environ()
		awsEnv := env[:0]
		for _, v := range env {
			if strings.HasPrefix(v, "AWS_") {
				awsEnv = append(awsEnv, v)
			}
		}
		sort.Strings(awsEnv)
		fmt.Println(strings.Join(awsEnv, "\n"))
		if strings.Contains(os.Getenv("AWS_SESSION_TOKEN"), "000000000002") {
			os.Exit(1)
		}
		os.Exit(0)
	}
	ctx, _ := mockOrg(mock.Ctx, "test1", "test2")
	var b bytes.Buffer
	log.SetOutput(&b)
	log.SetFlags(0)
	cmd := execCmd{
		Spec:   "test1,test2",
		Cmd:    os.Args[0],
		Args:   []string{"-test.run=TestExec"},
		Stdout: &b,
		Stderr: &b,
	}
	os.Setenv(testEnv, "1")
	_, err := cmd.Run(ctx)
	require.Equal(t, cli.ExitCode(1), err)
	want := cli.Dedent(`
		==> ACCOUNT 000000000001: test1
		AWS_ACCESS_KEY_ID=ASIAIOSFODNN7EXAMPLE
		AWS_DEFAULT_REGION=us-east-1
		AWS_SECRET_ACCESS_KEY=wJalrXUtnFEMI/K7MDENG/bPxRfiCYzEXAMPLEKEY
		AWS_SESSION_TOKEN=arn:aws:sts::000000000001:assumed-role/alice/alice
		==> ACCOUNT 000000000002: test2
		AWS_ACCESS_KEY_ID=ASIAIOSFODNN7EXAMPLE
		AWS_DEFAULT_REGION=us-east-1
		AWS_SECRET_ACCESS_KEY=wJalrXUtnFEMI/K7MDENG/bPxRfiCYzEXAMPLEKEY
		AWS_SESSION_TOKEN=arn:aws:sts::000000000002:assumed-role/alice/alice
		==> ERROR: exit status 1
		==> TOTAL ERRORS: 1 (0 due to invalid credentials)
	`)[1:]
	assert.Equal(t, want, b.String())
}
