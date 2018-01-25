package okta

import (
	"bytes"
	"encoding/base64"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSAMLParser(t *testing.T) {
	sa, err := samlAssertionFromHTML(samlResponse(assertion))
	require.NoError(t, err)
	require.Equal(t, assertion, string(sa))
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
	_, err = samlAssertionFromHTML(samlResponse(""))
	require.Equal(t, ErrNoSAMLResponse, err)
}

const assertion = `<?xml version="1.0" encoding="UTF-8"?>
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
</Response>`

func samlResponse(assertion string) *bytes.Buffer {
	var buf bytes.Buffer
	buf.WriteString("<!DOCTYPE html><html><head></head><body><form>" +
		`<input name="SAMLResponse" type="hidden" value="`)
	enc := base64.StdEncoding
	b := make([]byte, enc.EncodedLen(len(assertion)))
	base64.StdEncoding.Encode(b, []byte(assertion))
	buf.Write(b)
	buf.WriteString(`"/></form></body></html>`)
	return &buf
}
