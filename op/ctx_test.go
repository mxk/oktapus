package op

import (
	"os"
	"testing"

	"github.com/LuminalHQ/oktapus/mock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCtxAWS(t *testing.T) {
	os.Unsetenv(OktaHostEnv)
	os.Setenv(NoDaemonEnv, "1")
	ctx := NewCtx()

	ctx.Sess = mock.NewSession()
	assert.False(t, ctx.UseOkta())
	acs, err := ctx.Accounts("err")
	require.NoError(t, err)
	assert.NotEmpty(t, acs)
}

func TestCtxEnvMap(t *testing.T) {
	assert.NotEmpty(t, new(Ctx).EnvMap())
}
