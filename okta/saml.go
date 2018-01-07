package okta

import (
	"encoding/base64"
	"encoding/xml"
	"errors"
	"io"
	"io/ioutil"

	"golang.org/x/net/html"
)

var ErrNoSAMLResponse = errors.New("okta: SAMLResponse form input not found")

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
