package op

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
)

const iamPolicyVersion = "2012-10-17"

// Policy is an IAM policy document.
type Policy struct {
	Version   string `json:",omitempty"`
	ID        string `json:"Id,omitempty"`
	Statement []*Statement
}

// NewAssumeRolePolicy returns an AssumeRole policy document.
func NewAssumeRolePolicy(principal string) *Policy {
	s := &Statement{
		Effect:    "Allow",
		Principal: NewAWSPrincipal(principal),
		Action:    PolicyMultiVal{"sts:AssumeRole"},
	}
	if principal == "" {
		s.Effect = "Deny"
		s.Principal.AWS[0] = "*"
	}
	return &Policy{Version: iamPolicyVersion, Statement: []*Statement{s}}
}

// ParsePolicy decodes an IAM policy document.
func ParsePolicy(s *string) (*Policy, error) {
	if s == nil {
		return nil, errors.New("policy: missing policy document")
	}
	doc := strings.TrimSpace(*s)
	if strings.HasPrefix(doc, "%7B") {
		var err error
		if doc, err = url.QueryUnescape(doc); err != nil {
			return nil, err
		}
	}
	p := new(Policy)
	err := json.Unmarshal([]byte(doc), &p)
	if err != nil {
		p = nil
	} else if p.Version != "" && p.Version != iamPolicyVersion {
		err = fmt.Errorf("policy: unsupported policy version %q", p.Version)
		p = nil
	}
	return p, err
}

// Doc returns JSON representation of policy p.
func (p *Policy) Doc() *string {
	if p.Version == "" {
		p.Version = iamPolicyVersion
	}
	// Buffer used to avoid HTML escaping
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(p); err != nil {
		panic("policy: encode error: " + err.Error())
	}
	return aws.String(strings.TrimSuffix(buf.String(), "\n"))
}

// Statement is an IAM policy statement.
type Statement struct {
	SID          string         `json:"Sid,omitempty"`
	Effect       string         `json:""`
	Principal    *Principal     `json:",omitempty"`
	NotPrincipal *Principal     `json:",omitempty"`
	Action       PolicyMultiVal `json:",omitempty"`
	NotAction    PolicyMultiVal `json:",omitempty"`
	Resource     PolicyMultiVal `json:",omitempty"`
	NotResource  PolicyMultiVal `json:",omitempty"`
	Condition    ConditionMap   `json:",omitempty"`
}

// Principal specifies the entity to which a statement applies.
type Principal struct {
	PrincipalMap
	Any bool
}

// PrincipalMap is a non-wildcard principal value.
type PrincipalMap struct {
	AWS       PolicyMultiVal `json:",omitempty"`
	Federated PolicyMultiVal `json:",omitempty"`
	Service   PolicyMultiVal `json:",omitempty"`
}

// NewAWSPrincipal returns a new Principal containing the specified AWS ids.
func NewAWSPrincipal(ids ...string) *Principal {
	return &Principal{PrincipalMap: PrincipalMap{AWS: PolicyMultiVal(ids)}}
}

// MarshalJSON implements json.Marshaler interface.
func (p *Principal) MarshalJSON() ([]byte, error) {
	if p.Any {
		if len(p.AWS) != 0 || len(p.Federated) != 0 || len(p.Service) != 0 {
			return nil, errors.New("policy: principal wildcard set and not set")
		}
		return []byte(`"*"`), nil
	}
	return json.Marshal(&p.PrincipalMap)
}

// UnmarshalJSON implements json.Unmarshaler interface.
func (p *Principal) UnmarshalJSON(b []byte) error {
	err := json.Unmarshal(b, &p.PrincipalMap)
	if err != nil {
		var s string
		if err = json.Unmarshal(b, &s); err == nil {
			if s == "*" {
				p.Any = true
			} else {
				err = fmt.Errorf("policy: invalid principal value %q", s)
			}
		}
	}
	return err
}

// PolicyMultiVal is a JSON type that may be encoded either as a string or an
// array, depending on the number of entries.
type PolicyMultiVal []string

// MarshalJSON implements json.Marshaler interface.
func (v PolicyMultiVal) MarshalJSON() ([]byte, error) {
	if len(v) == 0 {
		return []byte(`[]`), nil
	}
	var buf bytes.Buffer
	var err error
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if len(v) == 1 {
		err = enc.Encode(v[0])
	} else {
		err = enc.Encode([]string(v))
	}
	return bytes.TrimSuffix(buf.Bytes(), []byte{'\n'}), err
}

// UnmarshalJSON implements json.Unmarshaler interface.
func (v *PolicyMultiVal) UnmarshalJSON(b []byte) error {
	var s string
	err := json.Unmarshal(b, &s)
	if err == nil {
		*v = append((*v)[:0], s)
	} else {
		t := []string(*v)[:0]
		err = json.Unmarshal(b, &t)
		*v = t
	}
	return err
}

// ConditionMap associates policy condition type with a set of conditions.
type ConditionMap map[string]Conditions

// Conditions contains one or more policy conditions of the same type.
type Conditions map[string]PolicyMultiVal
