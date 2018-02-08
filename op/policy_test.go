package op

import (
	"bytes"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const policyTpl = `{
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
		doc: fmt.Sprintf(policyTpl, "Deny", "*"),
		p: Policy{
			Version: iamPolicyVersion,
			Statement: []*Statement{{
				Effect:    "Deny",
				Principal: NewAWSPrincipal("*"),
				Action:    PolicyMultiVal{"sts:AssumeRole"},
			}},
		},
	}, {
		in:  "000000000000",
		doc: fmt.Sprintf(policyTpl, "Allow", "000000000000"),
		p: Policy{
			Version: iamPolicyVersion,
			Statement: []*Statement{{
				Effect:    "Allow",
				Principal: NewAWSPrincipal("000000000000"),
				Action:    PolicyMultiVal{"sts:AssumeRole"},
			}},
		},
	}, {
		in:  "test",
		doc: fmt.Sprintf(policyTpl, "Allow", "test"),
		p: Policy{
			Version: iamPolicyVersion,
			Statement: []*Statement{{
				Effect:    "Allow",
				Principal: NewAWSPrincipal("test"),
				Action:    PolicyMultiVal{"sts:AssumeRole"},
			}},
		},
	}}
	for _, test := range tests {
		p := NewAssumeRolePolicy(test.in)
		assert.Equal(t, &test.p, p, "in=%q", test.in)
		p.Version = ""

		var have, want bytes.Buffer
		require.NoError(t, json.Indent(&have, []byte(*p.Doc()), "", "  "))
		require.NoError(t, json.Indent(&want, []byte(test.doc), "", "  "))
		assert.Equal(t, want.String(), have.String())

		p, err := ParsePolicy(&test.doc)
		require.NoError(t, err)
		assert.Equal(t, &test.p, p)
	}
}

func TestParsePolicy(t *testing.T) {
	tpl := `{"Version":"%s"}`
	doc := fmt.Sprintf(tpl, iamPolicyVersion)
	_, err := ParsePolicy(&doc)
	assert.NoError(t, err)

	doc = fmt.Sprintf(tpl, "2018-02-08")
	_, err = ParsePolicy(&doc)
	assert.Error(t, err)

	_, err = ParsePolicy(nil)
	assert.Error(t, err)

	doc = `{
		"Version": "2012-10-17",
		"Id": "cd3ad3d9-2776-4ef1-a904-4c229d1642ee",
		"Statement": [{
			"Sid": "1",
			"Effect": "Allow",
			"Principal": "*",
			"NotPrincipal": {"AWS":"*"},
			"Action": "*",
			"NotAction": ["a","b"],
			"Resource": "*",
			"NotResource": ["c","d"],
			"Condition": {
				"Bool": {"aws:SecureTransport":"true"},
				"NumericLessThanEquals": {"s3:max-keys":"10"},
				"StringEquals": {"s3:x-amz-server-side-encryption":"AES256"}
			}
		},{
			"Effect": "Deny",
			"Action": ["<e>","f&"]
		}]
	}`
	p, err := ParsePolicy(&doc)
	assert.NoError(t, err)
	var want, have bytes.Buffer
	require.NoError(t, json.Indent(&want, []byte(doc), "", "  "))
	require.NoError(t, json.Indent(&have, []byte(*p.Doc()), "", "  "))
	assert.Equal(t, want.String(), have.String())
}

func TestPolicyPrincipal(t *testing.T) {
	tests := []*struct {
		p   Principal
		doc string
	}{{
		p:   Principal{Any: true},
		doc: `"*"`,
	}, {
		p:   Principal{PrincipalMap: PrincipalMap{AWS: PolicyMultiVal{"*"}}},
		doc: `{"AWS":"*"}`,
	}, {
		p:   Principal{PrincipalMap: PrincipalMap{Federated: PolicyMultiVal{"*"}}},
		doc: `{"Federated":"*"}`,
	}, {
		p:   Principal{PrincipalMap: PrincipalMap{Service: PolicyMultiVal{"*"}}},
		doc: `{"Service":"*"}`,
	}, {
		p: Principal{PrincipalMap: PrincipalMap{AWS: PolicyMultiVal{"a", "b"},
			Service: PolicyMultiVal{"c"}}},
		doc: `{"AWS":["a","b"],"Service":"c"}`,
	}, {
		p:   *NewAWSPrincipal(),
		doc: `{}`,
	}, {
		p:   *NewAWSPrincipal("a"),
		doc: `{"AWS":"a"}`,
	}, {
		p:   *NewAWSPrincipal("a", "b"),
		doc: `{"AWS":["a","b"]}`,
	}}
	for _, test := range tests {
		doc, err := json.Marshal(&test.p)
		require.NoError(t, err)
		assert.Equal(t, test.doc, string(doc))
		var p Principal
		require.NoError(t, json.Unmarshal(doc, &p))
		assert.Equal(t, &test.p, &p)
	}

	var p Principal
	require.Error(t, json.Unmarshal([]byte(`""`), &p))
	require.Error(t, json.Unmarshal([]byte(`"x"`), &p))

	p = *NewAWSPrincipal("")
	p.Any = true
	_, err := json.Marshal(&p)
	require.Error(t, err)
}

func TestPolicyMultiVal(t *testing.T) {
	tests := []*struct {
		in   []byte
		want PolicyMultiVal
	}{{
		[]byte(`[]`),
		PolicyMultiVal{},
	}, {
		[]byte(`""`),
		PolicyMultiVal{""},
	}, {
		[]byte(`"a"`),
		PolicyMultiVal{"a"},
	}, {
		[]byte(`["a","b"]`),
		PolicyMultiVal{"a", "b"},
	}}
	for _, test := range tests {
		var v PolicyMultiVal
		require.NoError(t, json.Unmarshal(test.in, &v))
		assert.Equal(t, test.want, v, "in=%#q", test.in)
		out, err := json.Marshal(v)
		require.NoError(t, err)
		assert.Equal(t, test.in, out)
	}
}
