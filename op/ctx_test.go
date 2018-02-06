package op

import (
	"os"
	"testing"

	"github.com/LuminalHQ/oktapus/mock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCtxAWS(t *testing.T) {
	os.Unsetenv("OKTA_ORG")
	os.Setenv("OKTAPUS_NO_DAEMON", "1")
	ctx := NewCtx()

	ctx.sess = mock.NewSession(true)
	assert.False(t, ctx.UseOkta())
	acs, err := ctx.Accounts("err")
	require.NoError(t, err)
	assert.NotEmpty(t, acs)
}

func TestCtxEnvMap(t *testing.T) {
	assert.NotEmpty(t, new(Ctx).EnvMap())
}
