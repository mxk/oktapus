package okta

import (
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/LuminalHQ/oktapus/awsgw"

	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/service/sts"
)

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

// newAWSAuth returns a SAML-based AWS authenticator. If roleARN is specified,
// Roles will only contain the matching role. If the role is not found, all
// roles are returned with ErrInvalidAWSRole.
func newAWSAuth(sa samlAssertion, roleARN string) (*AWSAuth, error) {
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
			auth.Roles, err = getRoles(at.Values, roleARN)
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
	} else if roleARN != "" && auth.Roles[0].Role != roleARN {
		err = ErrInvalidAWSRole
	}
	return auth, err
}

// GetCreds returns credentials that derive from the SAML assertion and the
// specified role.
func (a *AWSAuth) GetCreds(fn awsgw.AssumeRoleWithSAMLFunc, r awsRole) *credentials.Credentials {
	saml := base64.StdEncoding.EncodeToString(a.SAML)
	return credentials.NewCredentials(&awsgw.SAMLCredsProvider{
		AssumeRoleWithSAMLInput: sts.AssumeRoleWithSAMLInput{
			PrincipalArn:  &r.Principal,
			RoleArn:       &r.Role,
			SAMLAssertion: &saml,
		},
		API: fn,
	})
}

// awsRole represents one IdP/role ARN pair in the "Role" attribute.
type awsRole struct{ Principal, Role string }

// getRoles extracts AWS roles from SAML attribute values.
func getRoles(vals []string, matchARN string) ([]awsRole, error) {
	roles := make([]awsRole, len(vals))
	for i, v := range vals {
		j := strings.IndexByte(v, ',')
		if j < 20 || !strings.HasPrefix(v[j+1:], "arn:") {
			return nil, fmt.Errorf("okta: invalid AWS role in SAML (%s)", v)
		}
		r := awsRole{v[:j], v[j+1:]}
		if strings.Contains(r.Role, ":saml-provider/") {
			r.Principal, r.Role = r.Role, r.Principal
		}
		if roles[i] = r; r.Role == matchARN && i > 0 {
			roles[0], roles[i] = r, roles[0]
		}
	}
	if roles[0].Role == matchARN {
		roles = roles[:1]
	}
	return roles, nil
}
