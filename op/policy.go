package op

import (
	"encoding/json"
	"errors"

	"github.com/aws/aws-sdk-go/aws"
)

const iamPolicyVersion = "2012-10-17"

// Policy is an IAM policy document.
type Policy struct {
	Version   string
	Statement []*Statement
}

// NewAssumeRolePolicy returns an AssumeRole policy document.
func NewAssumeRolePolicy(principal string) *Policy {
	s := &Statement{
		Effect:    "Deny",
		Principal: &Principal{AWS: "*"},
		Action:    Actions{"sts:AssumeRole"},
	}
	p := &Policy{
		Version:   iamPolicyVersion,
		Statement: []*Statement{s},
	}
	if principal != "" {
		if isAWSAccountID(principal) {
			principal = "arn:aws:iam::" + principal + ":root"
		}
		s.Effect = "Allow"
		s.Principal.AWS = principal
	}
	return p
}

// ParsePolicy decodes an IAM policy document.
func ParsePolicy(doc *string) (*Policy, error) {
	if doc == nil {
		return nil, errors.New("missing policy document")
	}
	p := new(Policy)
	err := json.Unmarshal([]byte(*doc), &p)
	if err != nil {
		p = nil
	}
	return p, err
}

// Doc returns JSON representation of policy p.
func (p *Policy) Doc() *string {
	b, err := json.Marshal(p)
	if err != nil {
		panic("policy encode error: " + err.Error())
	}
	return aws.String(string(b))
}

// Statement is an IAM policy statement.
type Statement struct {
	Effect    string     `json:",omitempty"`
	Principal *Principal `json:",omitempty"`
	Action    Actions    `json:",omitempty"`
}

// Principal specifies the entity to which a statement applies.
type Principal struct {
	AWS string
}

// Actions specifies API calls that are allowed or denied by a statement.
type Actions []string

// MarshalJSON implements json.Marshaler interface.
func (a Actions) MarshalJSON() ([]byte, error) {
	switch len(a) {
	case 0:
		if a == nil {
			return []byte(`""`), nil
		}
		return []byte(`[]`), nil
	case 1:
		return json.Marshal(a[0])
	default:
		return json.Marshal([]string(a))
	}
}

// UnmarshalJSON implements json.Unmarshaler interface.
func (a *Actions) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err == nil {
		if *a = (*a)[:0]; s != "" {
			*a = append(*a, s)
		}
		return nil
	}
	v := []string(*a)[:0]
	err := json.Unmarshal(b, &v)
	if err != nil {
		v = nil
	}
	*a = v
	return err
}
