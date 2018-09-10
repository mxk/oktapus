package okta

import (
	"bytes"
	"encoding/base64"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewAWSAuth(t *testing.T) {
	auth, err := newAWSAuth(samlAssertion(assertion), "")
	require.NoError(t, err)
	want := &AWSAuth{
		Assertion: assertion,
		Roles: []awsRole{{
			Principal: "arn:aws:iam::000000000000:saml-provider/Okta",
			Role:      "arn:aws:iam::000000000000:role/OktapusGateway",
		}},
	}
	assert.Equal(t, want, auth)

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

	r, err = getRoles(in, "arn:aws:iam::000000000000:role/Role2")
	require.NoError(t, err)
	assert.Equal(t, want[1:], r)
}

func TestSAMLParser(t *testing.T) {
	sa, err := samlAssertionFromHTML(samlResponse(assertion))
	require.NoError(t, err)
	require.Equal(t, assertion, sa)
	attrs, err := sa.attrs()
	require.NoError(t, err)
	want := []*samlAttr{{
		Name:   "https://aws.amazon.com/SAML/Attributes/Role",
		Values: []string{"arn:aws:iam::000000000000:saml-provider/Okta,arn:aws:iam::000000000000:role/OktapusGateway"},
	}, {
		Name:   "https://aws.amazon.com/SAML/Attributes/RoleSessionName",
		Values: []string{"user@example.com"},
	}, {
		Name:   "https://aws.amazon.com/SAML/Attributes/SessionDuration",
		Values: []string{"43200"},
	}}
	assert.Equal(t, want, attrs)
}

func TestSAMLParserError(t *testing.T) {
	_, err := samlAssertionFromHTML(bytes.NewReader(nil))
	require.Equal(t, ErrNoSAMLResponse, err)
	_, err = samlAssertionFromHTML(samlResponse(nil))
	require.Equal(t, ErrNoSAMLResponse, err)
}

var assertion = samlAssertion(`<?xml version="1.0" encoding="UTF-8"?>
<Response>
<Assertion>
<AttributeStatement>
	<Attribute Name="https://aws.amazon.com/SAML/Attributes/Role">
		<AttributeValue>arn:aws:iam::000000000000:saml-provider/Okta,arn:aws:iam::000000000000:role/OktapusGateway</AttributeValue>
	</Attribute>
	<Attribute Name="https://aws.amazon.com/SAML/Attributes/RoleSessionName">
		<AttributeValue>user@example.com</AttributeValue>
	</Attribute>
	<Attribute Name="https://aws.amazon.com/SAML/Attributes/SessionDuration">
		<AttributeValue>43200</AttributeValue>
	</Attribute>
</AttributeStatement>
</Assertion>
</Response>`)

func samlResponse(assertion samlAssertion) *bytes.Buffer {
	var buf bytes.Buffer
	buf.WriteString("<!DOCTYPE html><html><head></head><body><form>" +
		`<input name="SAMLResponse" type="hidden" value="`)
	enc := base64.StdEncoding
	b := make([]byte, enc.EncodedLen(len(assertion)))
	base64.StdEncoding.Encode(b, assertion)
	buf.Write(b)
	buf.WriteString(`"/></form></body></html>`)
	return &buf
}
