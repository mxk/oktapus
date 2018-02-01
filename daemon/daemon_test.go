package daemon

import (
	"crypto/sha512"
	"encoding/base64"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSig(t *testing.T) {
	h := sha512.Sum512([]byte("A=1\nB=2\n"))
	s := base64.URLEncoding.EncodeToString(h[:12])
	assert.Equal(t, s, sig(map[string]string{
		"A": "1",
		"B": "2",
	}))
}
