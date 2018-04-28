package cmd

import (
	"fmt"
	"io/ioutil"
	"os"
	"sort"
	"strings"
	"testing"

	"github.com/LuminalHQ/oktapus/internal"
	"github.com/LuminalHQ/oktapus/op"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExec(t *testing.T) {
	if os.Getenv(op.AccountIDEnv) != "" {
		env := os.Environ()
		awsEnv := env[:0]
		for _, v := range env {
			if strings.HasPrefix(v, "AWS_") {
				awsEnv = append(awsEnv, v)
			}
		}
		sort.Strings(awsEnv)
		for _, v := range awsEnv {
			fmt.Println(v)
		}
		if os.Getenv(op.AccountNameEnv) == "test2" {
			os.Exit(1)
		}
		os.Exit(0)
	}

	ctx := newCtx("1", "2")
	r, w, err := os.Pipe()
	require.NoError(t, err)

	var runErr error
	done := make(chan struct{})
	go func() {
		defer func(stdout, stderr *os.File) {
			os.Stdout, os.Stderr = stdout, stderr
			w.Close()
			close(done)
		}(os.Stdout, os.Stderr)
		cmd := newCmd("exec").(*execCmd)
		args := []string{"test1,test2", os.Args[0], "-test.run=TestExec"}
		os.Stdout, os.Stderr = w, w
		runErr = cmd.Run(ctx, args)
	}()
	out, err := ioutil.ReadAll(r)
	<-done

	require.NoError(t, err)
	require.EqualError(t, runErr, "1 command failed (0 due to invalid credentials)")
	want := internal.Dedent(`
		===> Account 000000000001 (test1)
		AWS_ACCESS_KEY_ID=AKIAIOSFODNN7EXAMPLE
		AWS_ACCOUNT_ID=000000000001
		AWS_ACCOUNT_NAME=test1
		AWS_SECRET_ACCESS_KEY=wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY
		AWS_SESSION_TOKEN=arn:aws:sts::000000000001:assumed-role/user@example.com/user@example.com
		===> Account 000000000002 (test2)
		AWS_ACCESS_KEY_ID=AKIAIOSFODNN7EXAMPLE
		AWS_ACCOUNT_ID=000000000002
		AWS_ACCOUNT_NAME=test2
		AWS_SECRET_ACCESS_KEY=wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY
		AWS_SESSION_TOKEN=arn:aws:sts::000000000002:assumed-role/user@example.com/user@example.com
		===> ERROR: exit status 1
	`)[1:]
	assert.Equal(t, want, string(out))
}
