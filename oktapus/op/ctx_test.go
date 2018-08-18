package op

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha512"
	"encoding/gob"
	"encoding/hex"
	"io/ioutil"
	"os"
	"testing"

	"github.com/LuminalHQ/cloudcover/oktapus/account"
	"github.com/LuminalHQ/cloudcover/oktapus/mock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCtx(t *testing.T) {
	tmp, err := ioutil.TempFile("", "ctx_test.")
	require.NoError(t, err)
	defer func() {
		tmp.Close()
		assert.NoError(t, os.Remove(tmp.Name()))
	}()
	tmp.WriteString("aws 100000000000 external\n")
	require.NoError(t, tmp.Close())

	ctx := NewCtx()
	ctx.AliasFile = tmp.Name()

	w := mock.NewAWS(mock.Ctx, mock.NewOrg(mock.Ctx, "master", "test1", "test2"))
	require.NoError(t, ctx.Init(&w.Cfg))
	require.NoError(t, ctx.Refresh())

	acs := ctx.Accounts()
	require.Len(t, acs, 4)
}

func TestMasterExternalID(t *testing.T) {
	var ctx Ctx
	assert.Panics(t, func() { ctx.MasterExternalID() })
	ctx.mode = IAM
	ctx.dir.Org = account.Org{
		ID:          "o-test",
		MasterID:    "000000000000",
		MasterEmail: "master@example.com",
	}
	h := hmac.New(sha512.New512_256, []byte("o-test"))
	h.Write([]byte("oktapus:000000000000:master@example.com"))
	assert.Equal(t, hex.EncodeToString(h.Sum(nil)), *ctx.MasterExternalID())
}

func TestSavedCtx(t *testing.T) {
	var b bytes.Buffer
	require.NoError(t, gob.NewEncoder(&b).Encode(new(SavedCtx)))
	var v SavedCtx
	require.NoError(t, gob.NewDecoder(&b).Decode(&v))
}

func TestSetEnvFields(t *testing.T) {
	type V struct {
		S1 string `env:"_S1"`
		S2 string `env:"_S2"`
		B1 bool   `env:"_B1"`
		B2 bool   `env:"_B2"`
		B3 bool   `env:"_B3"`
	}
	env := map[string]string{
		"_S2": "s2",
		"_B2": "",
		"_B3": "0",
	}
	defer func() {
		for k := range env {
			os.Unsetenv(k)
		}
	}()
	for k, v := range env {
		os.Setenv(k, v)
	}
	var have V
	setEnvFields(&have)
	want := V{S2: "s2", B2: true}
	assert.Equal(t, want, have)
}
