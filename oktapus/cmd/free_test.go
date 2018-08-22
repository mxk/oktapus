package cmd

import (
	"testing"

	"github.com/LuminalHQ/cloudcover/oktapus/mock"
	"github.com/LuminalHQ/cloudcover/oktapus/op"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFree(t *testing.T) {
	ctx, w := mockOrg(mock.Ctx, "test1", "test2")
	setCtl(w, op.Ctl{Owner: "alice"}, "1")
	setCtl(w, op.Ctl{Owner: "bob"}, "2")

	cmd := freeCmd{Spec: "test1,test2"}
	out, err := cmd.Run(ctx)
	require.NoError(t, err)
	want := []*ownerOutput{{
		Account: "000000000001",
		Name:    "test1",
		Result:  "OK",
	}}
	assert.Equal(t, want, out)
}
