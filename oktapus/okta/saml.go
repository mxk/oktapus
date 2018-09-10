package okta

import (
	"encoding/base64"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"strings"

	"github.com/LuminalHQ/cloudcover/x/arn"
	"golang.org/x/net/html"
)

// Possible errors returned when parsing AWS SAML assertion.
var (
	ErrNoSAMLResponse = errors.New("okta: SAMLResponse form input not found")
	ErrNoAWSRoles     = errors.New("okta: no AWS roles in SAML assertion")
	ErrInvalidAWSRole = errors.New("okta: specified role is not available")
)

// AWSAuth contains authentication data for AWS.
type AWSAuth struct {
	Assertion samlAssertion
	Roles     []awsRole
}

// newAWSAuth returns a SAML-based AWS authenticator. If role is specified,
// Roles will only contain the matching role. If the role is not found, all
// roles are returned with ErrInvalidAWSRole.
func newAWSAuth(sa samlAssertion, role arn.ARN) (*AWSAuth, error) {
	attrs, err := sa.attrs()
	if err != nil {
		return nil, err
	}
	auth := &AWSAuth{Assertion: sa}
	for _, at := range attrs {
		if at.Name == "https://aws.amazon.com/SAML/Attributes/Role" {
			auth.Roles, err = getRoles(at.Values, role)
			break
		}
	}
	if err == nil {
		if len(auth.Roles) == 0 {
			err = ErrNoAWSRoles
		} else if role != "" && auth.Roles[0].Role != role {
			err = ErrInvalidAWSRole
		}
	}
	return auth, err
}

// awsRole represents one IdP/role ARN pair in the "Role" attribute.
type awsRole struct{ Principal, Role arn.ARN }

// getRoles extracts AWS roles from SAML attribute values.
func getRoles(vals []string, match arn.ARN) ([]awsRole, error) {
	roles := make([]awsRole, len(vals))
	for i, v := range vals {
		r := &roles[i]
		if j := strings.IndexByte(v, ','); j > 0 {
			r.Principal, r.Role = arn.ARN(v[:j]), arn.ARN(v[j+1:])
		}
		if !r.Principal.Valid() || !r.Role.Valid() {
			return nil, fmt.Errorf("okta: invalid AWS role in SAML (%s)", v)
		}
		if r.Role.Type() == "saml-provider" {
			r.Principal, r.Role = r.Role, r.Principal
		}
		if r.Role == match {
			return roles[i : i+1], nil
		}
	}
	return roles, nil
}

// samlAssertion is a SAML assertion in its decoded XML form.
type samlAssertion []byte

// samlAssertionFromHTML returns the decoded SAMLResponse form input from Okta's
// SSO response. This is apparently the official and only way of getting a SAML
// assertion from Okta (as used by their okta-aws-cli-assume-role tool).
func samlAssertionFromHTML(r io.Reader) (samlAssertion, error) {
	// Response size is limited to 1 MB. The reader is fully drained at the end
	// because the tokenizer stops scanning once SAMLResponse is found.
	if _, ok := r.(*io.LimitedReader); !ok {
		r = io.LimitReader(r, 1024*1024)
	}
	defer io.Copy(ioutil.Discard, r)
	z := html.NewTokenizer(r)
	for {
		switch t := z.Next(); t {
		case html.ErrorToken:
			err := z.Err()
			if err == io.EOF {
				err = ErrNoSAMLResponse
			}
			return nil, err
		case html.StartTagToken, html.SelfClosingTagToken:
			tag, moreAttr := z.TagName()
			if len(tag) != 5 || string(tag) != "input" {
				continue
			}
			var nameMatch, typeMatch bool
			var k, v, value []byte
			for moreAttr {
				switch k, v, moreAttr = z.TagAttr(); string(k) {
				case "name":
					nameMatch = string(v) == "SAMLResponse"
					moreAttr = moreAttr && nameMatch
				case "type":
					typeMatch = string(v) == "hidden"
					moreAttr = moreAttr && typeMatch
				case "value":
					value = v
				}
			}
			if nameMatch && typeMatch {
				if len(value) == 0 {
					return nil, ErrNoSAMLResponse
				}
				dec := base64.StdEncoding
				buf := make([]byte, dec.DecodedLen(len(value)))
				n, err := dec.Decode(buf, value)
				return buf[:n], err
			}
		}
	}
}

// Encode returns the base64 encoding of SAML assertion sa.
func (sa samlAssertion) Encode() string {
	return base64.StdEncoding.EncodeToString(sa)
}

// samlAttr is a SAML assertion attribute.
type samlAttr struct {
	Name   string   `xml:",attr"`
	Values []string `xml:"AttributeValue"`
}

// attrs returns all attributes from SAML assertion sa.
func (sa samlAssertion) attrs() ([]*samlAttr, error) {
	var assert struct {
		Attrs []*samlAttr `xml:"Assertion>AttributeStatement>Attribute"`
	}
	err := xml.Unmarshal(sa, &assert)
	return assert.Attrs, err
}
