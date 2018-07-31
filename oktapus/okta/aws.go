package okta

import (
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/LuminalHQ/cloudcover/oktapus/creds"
	"github.com/LuminalHQ/cloudcover/x/arn"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sts"
)

// Possible errors returned when parsing AWS SAML assertion.
var (
	ErrNoAWSRoles     = errors.New("okta: no AWS roles in SAML assertion")
	ErrInvalidAWSRole = errors.New("okta: specified role is not available")
)

// AWSAuth contains authentication data for AWS.
type AWSAuth struct {
	SAML            samlAssertion
	Roles           []awsRole
	RoleSessionName string
	SessionDuration time.Duration
}

// newAWSAuth returns a SAML-based AWS authenticator. If role is specified,
// Roles will only contain the matching role. If the role is not found, all
// roles are returned with ErrInvalidAWSRole.
func newAWSAuth(sa samlAssertion, role arn.ARN) (*AWSAuth, error) {
	attrs, err := sa.attrs()
	if err != nil {
		return nil, err
	}
	auth := &AWSAuth{SAML: sa}
	for _, at := range attrs {
		if len(at.Values) == 0 {
			return nil, fmt.Errorf("okta: empty SAML attribute (%s)", at.Name)
		}
		switch at.Name {
		case "https://aws.amazon.com/SAML/Attributes/Role":
			auth.Roles, err = getRoles(at.Values, role)
		case "https://aws.amazon.com/SAML/Attributes/RoleSessionName":
			auth.RoleSessionName = at.Values[0]
		case "https://aws.amazon.com/SAML/Attributes/SessionDuration":
			var d uint64
			d, err = strconv.ParseUint(at.Values[0], 10, 32)
			auth.SessionDuration = time.Duration(d) * time.Second
		}
		if err != nil {
			return nil, err
		}
	}
	if len(auth.Roles) == 0 {
		err = ErrNoAWSRoles
	} else if role != "" && auth.Roles[0].Role != role {
		err = ErrInvalidAWSRole
	}
	return auth, err
}

// Creds returns credentials that derive from the SAML assertion and the
// specified role.
func (a *AWSAuth) Creds(cfg *aws.Config, r awsRole) *creds.Provider {
	c := sts.New(*cfg)
	creds.Set(c.Client, aws.AnonymousCredentials)
	in := &sts.AssumeRoleWithSAMLInput{
		DurationSeconds: aws.Int64(int64(a.SessionDuration.Seconds())),
		PrincipalArn:    arn.String(r.Principal),
		RoleArn:         arn.String(r.Role),
		SAMLAssertion:   aws.String(base64.StdEncoding.EncodeToString(a.SAML)),
	}
	return creds.RenewableProvider(func() (cr aws.Credentials, err error) {
		out, err := c.AssumeRoleWithSAMLRequest(in).Send()
		if err == nil {
			cr = creds.FromSTS(out.Credentials)
		}
		cr.Source = "Okta"
		return
	})
}

// Use configures in to use the SAML assertion and the specified role.
func (a *AWSAuth) Use(r awsRole, in *sts.AssumeRoleWithSAMLInput) {
	// TODO: Duration?
	in.PrincipalArn = arn.String(r.Principal)
	in.RoleArn = arn.String(r.Role)
	in.SAMLAssertion = aws.String(base64.StdEncoding.EncodeToString(a.SAML))
}

// awsRole represents one IdP/role ARN pair in the "Role" attribute.
type awsRole struct{ Principal, Role arn.ARN }

// getRoles extracts AWS roles from SAML attribute values.
func getRoles(vals []string, match arn.ARN) ([]awsRole, error) {
	roles := make([]awsRole, len(vals))
	for i, v := range vals {
		j := strings.IndexByte(v, ',')
		if j < 20 || !strings.HasPrefix(v[j+1:], "arn:") {
			return nil, fmt.Errorf("okta: invalid AWS role in SAML (%s)", v)
		}
		r := awsRole{arn.ARN(v[:j]), arn.ARN(v[j+1:])}
		if r.Role.Type() == "saml-provider" {
			r.Principal, r.Role = r.Role, r.Principal
		}
		if roles[i] = r; r.Role == match && i > 0 {
			roles[0], roles[i] = r, roles[0]
		}
	}
	if roles[0].Role == match {
		roles = roles[:1]
	}
	return roles, nil
}
