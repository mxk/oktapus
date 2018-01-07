package awsgw

import (
	"time"

	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/service/sts"
)

type (
	AssumeRoleWithSAMLFunc func(*sts.AssumeRoleWithSAMLInput) (*sts.AssumeRoleWithSAMLOutput, error)
	AssumeRoleFunc         func(*sts.AssumeRoleInput) (*sts.AssumeRoleOutput, error)
)

// SAMLCredsProvider is a credentials.Provider that exchanges a SAML assertion
// for temporary security credentials.
type SAMLCredsProvider struct {
	baseProvider
	sts.AssumeRoleWithSAMLInput
	API AssumeRoleWithSAMLFunc
}

func (p *SAMLCredsProvider) Retrieve() (credentials.Value, error) {
	return retrieve(p)
}

func (SAMLCredsProvider) name() string {
	return "SAMLCredsProvider"
}

func (p *SAMLCredsProvider) getCreds() (*sts.Credentials, error) {
	out, err := p.API(&p.AssumeRoleWithSAMLInput)
	if out != nil {
		return out.Credentials, err
	}
	return nil, err
}

// AssumeRoleCredsProvider is a credentials.Provider that calls sts:AssumeRole
// to get temporary security credentials. It's a more direct implementation of
// stscreds.AssumeRoleProvider.
type AssumeRoleCredsProvider struct {
	baseProvider
	sts.AssumeRoleInput
	API AssumeRoleFunc
}

func (p *AssumeRoleCredsProvider) Retrieve() (credentials.Value, error) {
	return retrieve(p)
}

func (AssumeRoleCredsProvider) name() string {
	return "AssumeRoleCredsProvider"
}

func (p *AssumeRoleCredsProvider) getCreds() (*sts.Credentials, error) {
	out, err := p.API(&p.AssumeRoleInput)
	if out != nil {
		return out.Credentials, err
	}
	return nil, err
}

// credsProvider defines methods for credentials retrieval logic in retrieve().
type credsProvider interface {
	name() string
	getError() error
	setError(awserr.Error)
	getCreds() (*sts.Credentials, error)
	setExpiration(time.Time)
}

// retrieve gets sts.Credentials, converts them into credentials.Value, and
// updates provider's expiration time. Provider has the option of storing a
// permanent error to avoid making unnecessary API calls in a provider chain.
func retrieve(p credsProvider) (credentials.Value, error) {
	v := credentials.Value{ProviderName: p.name()}
	if err := p.getError(); err != nil {
		return v, err
	}
	c, err := p.getCreds()
	if err == nil {
		v.AccessKeyID = *c.AccessKeyId
		v.SecretAccessKey = *c.SecretAccessKey
		v.SessionToken = *c.SessionToken
		p.setExpiration(c.Expiration.Add(-1 * time.Minute))
	} else if e, ok := err.(awserr.Error); ok {
		p.setError(e)
	}
	return v, err
}

// baseProvider replaces credentials.Expiry to provide access to the actual
// expiration time and implements credsProvider error interface.
type baseProvider struct {
	Expiration time.Time
	Err        error
}

func (p *baseProvider) IsExpired() bool {
	return p.Expiration.Before(time.Now())
}

func (p *baseProvider) setExpiration(ex time.Time) {
	p.Expiration = ex
}

func (p *baseProvider) getError() error {
	return p.Err
}

func (p *baseProvider) setError(err awserr.Error) {
	if _, ok := permanentErrors[err.Code()]; ok {
		p.Err = err
	}
}

// permanentErrors defines errors that will never be resolved by retrying the
// same API call. These can be safely cached and returned for all subsequent
// credential requests from the same provider.
var permanentErrors = map[string]struct{}{
	sts.ErrCodeExpiredTokenException:            {},
	sts.ErrCodeIDPRejectedClaimException:        {},
	sts.ErrCodeInvalidIdentityTokenException:    {},
	sts.ErrCodeMalformedPolicyDocumentException: {},
	sts.ErrCodePackedPolicyTooLargeException:    {},
}
