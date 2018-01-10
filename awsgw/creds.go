package awsgw

import (
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/service/sts"
)

// AssumeRoleWithSAMLFunc is the signature of sts:AssumeRoleWithSAML API call.
type AssumeRoleWithSAMLFunc func(*sts.AssumeRoleWithSAMLInput) (*sts.AssumeRoleWithSAMLOutput, error)

// AssumeRoleFunc is the signature of sts:AssumeRole API call.
type AssumeRoleFunc func(*sts.AssumeRoleInput) (*sts.AssumeRoleOutput, error)

// CredsProvider extends credentials.Provider interface. This allows credentials
// to be cached and reused across process invocations.
type CredsProvider interface {
	credentials.Provider
	Expires() time.Time
}

// ChainCreds is a re-implementation of credentials.ChainProvider that satisfies
// CredsProvider interface.
type ChainCreds struct {
	chain  []CredsProvider
	active CredsProvider
}

// NewChainCreds combines multiple CredsProviders into one. StaticCreds are
// added only if they expire after minExp time.
func NewChainCreds(minExp time.Time, cp ...CredsProvider) *ChainCreds {
	c := new(ChainCreds)
	for _, p := range cp {
		_, static := p.(*StaticCreds)
		if p != nil && (!static || p.Expires().After(minExp)) {
			c.chain = append(c.chain, p)
		}
	}
	return c
}

// Retrieve returns new credentials from the first available provider.
func (c *ChainCreds) Retrieve() (credentials.Value, error) {
	var errs []error
	for _, p := range c.chain {
		v, err := p.Retrieve()
		if err == nil {
			c.active = p
			return v, nil
		}
		errs = append(errs, err)
	}
	c.active = nil
	err := awserr.NewBatchError("NoCredentialProviders",
		"no valid providers in chain", errs)
	return credentials.Value{ProviderName: "ChainCreds"}, err
}

// IsExpired returns expiration status of the current provider.
func (c *ChainCreds) IsExpired() bool {
	if c.active != nil {
		return c.active.IsExpired()
	}
	return true
}

// Expires returns expiration time of the current provider.
func (c *ChainCreds) Expires() time.Time {
	if c.active != nil {
		return c.active.Expires()
	}
	return time.Time{}
}

// StaticCreds is a CredsProvider that can be serialized for external storage.
type StaticCreds struct {
	credentials.Value
	Exp time.Time
}

// NewStaticCreds returns a static CredsProvider, which uses cached credentials
// from c and expiration time exp.
func NewStaticCreds(c *credentials.Credentials, exp time.Time) *StaticCreds {
	if c == nil || exp.IsZero() || c.IsExpired() {
		return nil
	}
	// This is a race, but the assumption is that Get does not call Retrieve()
	// on its provider when IsExpired() is false. We want fast access to the
	// cached credentials, not to get new ones.
	if v, err := c.Get(); err == nil {
		v.ProviderName = "" // No point in serializing this
		return &StaticCreds{v, exp}
	}
	return nil
}

// Retrieve returns static credentials until they expire.
func (c *StaticCreds) Retrieve() (credentials.Value, error) {
	var err error
	if c.IsExpired() {
		c.Value = credentials.Value{}
		err = fmt.Errorf("awsgw: static creds expired on %v", c.Exp)
	}
	c.ProviderName = "StaticCreds"
	return c.Value, err
}

// IsExpired returns true once the credentials have expired.
func (c *StaticCreds) IsExpired() bool {
	return !c.Exp.After(time.Now())
}

// Expires returns the expiration time.
func (c *StaticCreds) Expires() time.Time {
	return c.Exp
}

// SAMLCreds is a CredsProvider that exchanges a SAML assertion for temporary
// security credentials.
type SAMLCreds struct {
	stsCreds
	sts.AssumeRoleWithSAMLInput
	api AssumeRoleWithSAMLFunc
}

// NewSAMLCreds returns a new SAML-based CredsProvider.
func NewSAMLCreds(api AssumeRoleWithSAMLFunc, principal, role, saml string) *SAMLCreds {
	return &SAMLCreds{
		AssumeRoleWithSAMLInput: sts.AssumeRoleWithSAMLInput{
			PrincipalArn:  aws.String(principal),
			RoleArn:       aws.String(role),
			SAMLAssertion: aws.String(saml),
		},
		api: api,
	}
}

// Retrieve returns new credentials from STS.
func (c *SAMLCreds) Retrieve() (credentials.Value, error) {
	return retrieve(c)
}

func (*SAMLCreds) name() string {
	return "SAMLCreds"
}

func (c *SAMLCreds) getCreds() (*sts.Credentials, error) {
	out, err := c.api(&c.AssumeRoleWithSAMLInput)
	if out != nil {
		return out.Credentials, err
	}
	return nil, err
}

// AssumeRoleCreds is a CredsProvider that calls sts:AssumeRole to get temporary
// security credentials.
type AssumeRoleCreds struct {
	stsCreds
	sts.AssumeRoleInput
	api AssumeRoleFunc
}

// NewAssumeRoleCreds returns a new role-based CredsProvider.
func NewAssumeRoleCreds(api AssumeRoleFunc, role, roleSessionName string) *AssumeRoleCreds {
	return &AssumeRoleCreds{
		AssumeRoleInput: sts.AssumeRoleInput{
			RoleArn:         aws.String(role),
			RoleSessionName: aws.String(roleSessionName),
		},
		api: api,
	}
}

// Retrieve returns new credentials from STS.
func (c *AssumeRoleCreds) Retrieve() (credentials.Value, error) {
	return retrieve(c)
}

func (*AssumeRoleCreds) name() string {
	return "AssumeRoleCreds"
}

func (c *AssumeRoleCreds) getCreds() (*sts.Credentials, error) {
	out, err := c.api(&c.AssumeRoleInput)
	if out != nil {
		return out.Credentials, err
	}
	return nil, err
}

// stsCredsProvider defines the interface used by retrieve().
type stsCredsProvider interface {
	name() string
	getCreds() (*sts.Credentials, error)
	getError() error
	setError(awserr.Error)
	setExpiration(time.Time)
}

// retrieve gets sts.Credentials, converts them into credentials.Value, and
// updates provider's expiration time. Provider has the option of storing a
// permanent error to avoid making unnecessary API calls in a provider chain.
func retrieve(p stsCredsProvider) (credentials.Value, error) {
	v := credentials.Value{ProviderName: p.name()}
	p.setExpiration(time.Time{})
	if err := p.getError(); err != nil {
		return v, err
	}
	c, err := p.getCreds()
	if err == nil {
		v.AccessKeyID = *c.AccessKeyId
		v.SecretAccessKey = *c.SecretAccessKey
		v.SessionToken = *c.SessionToken
		p.setExpiration(c.Expiration.Add(-time.Minute))
	} else if e, ok := err.(awserr.Error); ok {
		p.setError(e)
	}
	return v, err
}

// stsCreds implements CredsProvider's expiration interface and
// stsCredsProvider's error interface.
type stsCreds struct {
	exp time.Time
	err error
}

// IsExpired returns true once the credentials have expired.
func (c *stsCreds) IsExpired() bool {
	return !c.exp.After(time.Now())
}

// Expires returns the expiration time.
func (c *stsCreds) Expires() time.Time {
	return c.exp
}

func (c *stsCreds) getError() error {
	return c.err
}

func (c *stsCreds) setError(err awserr.Error) {
	if _, ok := permanentErrors[err.Code()]; ok {
		c.err = err
	}
}

func (c *stsCreds) setExpiration(exp time.Time) {
	c.exp = exp
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
