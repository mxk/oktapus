package op

import (
	"bytes"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const tpl = `{
	"Version": "` + iamPolicyVersion + `",
	"Statement": [{
		"Effect": "%s",
		"Principal": {"AWS": "%s"},
		"Action": "sts:AssumeRole"
	}]
}`

func TestAssumeRolePolicy(t *testing.T) {
	tests := []*struct {
		in  string
		doc string
		p   Policy
	}{{
		in:  "",
		doc: fmt.Sprintf(tpl, "Deny", "*"),
		p: Policy{
			Version: iamPolicyVersion,
			Statement: []*Statement{{
				Effect:    "Deny",
				Principal: &Principal{AWS: "*"},
				Action:    Actions{"sts:AssumeRole"},
			}},
		},
	}, {
		in:  "000000000000",
		doc: fmt.Sprintf(tpl, "Allow", "arn:aws:iam::000000000000:root"),
		p: Policy{
			Version: iamPolicyVersion,
			Statement: []*Statement{{
				Effect:    "Allow",
				Principal: &Principal{AWS: "arn:aws:iam::000000000000:root"},
				Action:    Actions{"sts:AssumeRole"},
			}},
		},
	}, {
		in:  "test",
		doc: fmt.Sprintf(tpl, "Allow", "test"),
		p: Policy{
			Version: iamPolicyVersion,
			Statement: []*Statement{{
				Effect:    "Allow",
				Principal: &Principal{AWS: "test"},
				Action:    Actions{"sts:AssumeRole"},
			}},
		},
	}}
	for _, test := range tests {
		p := NewAssumeRolePolicy(test.in)
		assert.Equal(t, &test.p, p, "in=%q", test.in)

		var have, want bytes.Buffer
		require.NoError(t, json.Indent(&have, []byte(*p.Doc()), "", "  "))
		require.NoError(t, json.Indent(&want, []byte(test.doc), "", "  "))
		assert.Equal(t, want.String(), have.String())

		p, err := ParsePolicy(&test.doc)
		require.NoError(t, err)
		assert.Equal(t, &test.p, p)
	}
}

func TestAction(t *testing.T) {
	tests := []*struct {
		in   []byte
		want Actions
	}{{
		[]byte(`""`),
		nil,
	}, {
		[]byte(`[]`),
		Actions{},
	}, {
		[]byte(`"test"`),
		Actions{"test"},
	}, {
		[]byte(`["a","b"]`),
		Actions{"a", "b"},
	}}
	for _, test := range tests {
		var a Actions
		require.NoError(t, json.Unmarshal(test.in, &a))
		assert.Equal(t, test.want, a, "in=%#q", test.in)
		out, err := json.Marshal(a)
		require.NoError(t, err)
		assert.Equal(t, test.in, out)
	}
}
