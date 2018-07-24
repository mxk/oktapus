package cmd

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInit(t *testing.T) {
	cmd := initCmd{Spec: "test1,test2"}
	ctx := newCtx("1")
	out, err := cmd.Call(ctx)
	require.NoError(t, err)
	want := []*resultsOutput{{
		AccountID: "000000000001",
		Name:      "test1",
		Result:    "ERROR: already initialized",
	}, {
		AccountID: "000000000002",
		Name:      "test2",
		Result:    "OK",
	}}
	assert.Equal(t, want, out)
}
