package op

import (
	"bytes"
	"testing"

	"github.com/LuminalHQ/cloudcover/oktapus/okta"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAuthn(t *testing.T) {
	var r, w bytes.Buffer
	authn := newTermAuthn("")
	authn.r, authn.w = &r, &w

	r.WriteString("user\n")
	user, err := authn.Username()
	require.NoError(t, err)
	assert.Equal(t, "user", user)

	r.WriteString("pass\n")
	pass, err := authn.Password()
	require.NoError(t, err)
	assert.Equal(t, "pass", pass)

	r.WriteString("2\n")
	choice := []okta.Choice{
		&okta.Factor{
			ID:         "1",
			FactorType: "token:software:totp",
			Provider:   "GOOGLE",
		},
		&okta.Factor{
			ID:         "2",
			FactorType: "question",
			Provider:   "OKTA",
		},
	}
	c, err := authn.Select(choice)
	require.NoError(t, err)
	assert.True(t, c == choice[1])

	c, err = authn.Select(choice[:1])
	require.NoError(t, err)
	assert.True(t, c == choice[0])

	r.WriteString("answer\n")
	ans, err := authn.Input(choice[1])
	require.NoError(t, err)
	assert.Equal(t, "answer", ans)

	authn.Notify("\nthis is a %s\n", "test")
	assert.Contains(t, w.String(), "\nthis is a test\n")
}
