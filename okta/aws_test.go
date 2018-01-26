package okta

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewAWSAuth(t *testing.T) {
	auth, err := newAWSAuth(samlAssertion(assertion), "")
	require.NoError(t, err)
	want := &AWSAuth{
		SAML: assertion,
		Roles: []awsRole{{
			Principal: "arn:aws:iam::000000000000:saml-provider/Okta",
			Role:      "arn:aws:iam::000000000000:role/OktapusGateway",
		}},
		RoleSessionName: "user@example.com",
		SessionDuration: 43200 * time.Second,
	}
	assert.Equal(t, want, auth)
	assert.NotNil(t, auth.GetCreds(nil, auth.Roles[0]))

	_, err = newAWSAuth(samlAssertion(assertion), "InvalidRole")
	assert.Equal(t, ErrInvalidAWSRole, err)
}

func TestGetRoles(t *testing.T) {
	in := []string{
		"arn:aws:iam::000000000000:saml-provider/Okta,arn:aws:iam::000000000000:role/Role1",
		"arn:aws:iam::000000000000:role/Role2,arn:aws:iam::000000000000:saml-provider/Okta",
	}
	r, err := getRoles(in, "")
	require.NoError(t, err)
	want := []awsRole{{
		Principal: "arn:aws:iam::000000000000:saml-provider/Okta",
		Role:      "arn:aws:iam::000000000000:role/Role1",
	}, {
		Principal: "arn:aws:iam::000000000000:saml-provider/Okta",
		Role:      "arn:aws:iam::000000000000:role/Role2",
	}}
	assert.Equal(t, want, r)

	want[0], want[1] = want[1], want[0]
	r, err = getRoles(in, "arn:aws:iam::000000000000:role/Role2")
	require.NoError(t, err)
	assert.Equal(t, want, r[:cap(r)])
}
