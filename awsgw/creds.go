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

// CredsProvider extends credentials.Provider interface. It allows credentials
// to be cached and reused across process invocations.
type CredsProvider interface {
	credentials.Provider
	Expires() time.Time
	Save(errExp time.Time) *StaticCreds
}

// ChainCreds is a re-implementation of credentials.ChainProvider that satisfies
// CredsProvider interface.
type ChainCreds struct {
	chain  []CredsProvider
	active CredsProvider
	last   CredsProvider
}

// NewChainCreds combines multiple CredsProviders into one. StaticCreds are
// added only if they expire after minExp time or if they contain an error.
func NewChainCreds(minExp time.Time, cp ...CredsProvider) *ChainCreds {
	c := new(ChainCreds)
	for i, p := range cp {
		s, static := p.(*StaticCreds)
		if p != nil && (!static || s.Expires().After(minExp) || s.Err != nil) {
			if c.chain == nil {
				c.chain = make([]CredsProvider, 0, len(cp)-i)
			}
			c.chain = append(c.chain, p)
		}
	}
	return c
}

// Retrieve returns new credentials from the first available provider. Unexpired
// StaticCreds are not skipped.
func (c *ChainCreds) Retrieve() (credentials.Value, error) {
	var errs []error
	c.active, c.last = nil, nil
	for i, p := range c.chain {
		if p != nil {
			v, err := p.Retrieve()
			if err == nil {
				c.active = p
				return v, nil
			}
			if _, ok := p.(*StaticCreds); ok {
				if !p.IsExpired() {
					c.active = p
					return v, err
				}
				c.chain[i] = nil
			} else {
				c.last = p
			}
			errs = append(errs, err)
		}
	}
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

// Save returns a static copy of the most recently retrieved credentials.
func (c *ChainCreds) Save(errExp time.Time) *StaticCreds {
	if c.active != nil {
		return c.active.Save(errExp)
	}
	if c.last != nil {
		return c.last.Save(errExp)
	}
	return nil
}

// StaticCreds is a CredsProvider that can be encoded for external storage. If
// Err is set, Retrieve() returns that error forever.
type StaticCreds struct {
	credentials.Value
	Exp time.Time
	Err error
}

// Retrieve returns static credentials until they expire.
func (c *StaticCreds) Retrieve() (credentials.Value, error) {
	if c.Err == nil && c.IsExpired() {
		c.Value = credentials.Value{}
		c.Err = fmt.Errorf("awsgw: static creds expired on %v", c.Exp)
	}
	c.ProviderName = "StaticCreds"
	return c.Value, c.Err
}

// IsExpired returns true once the credentials have expired.
func (c *StaticCreds) IsExpired() bool {
	return !c.Exp.After(time.Now())
}

// Expires returns the expiration time.
func (c *StaticCreds) Expires() time.Time {
	return c.Exp
}

// Save returns c without modifying expiration time regardless of the error
// status. This allows an existing error to be re-encoded with the original
// expiration time.
func (c *StaticCreds) Save(errExp time.Time) *StaticCreds {
	if c.Err != nil {
		c.Value = credentials.Value{}
		c.Err = encodableError(c.Err)
	} else {
		c.ProviderName = "" // No point in serializing this
	}
	return c
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
	setError(err awserr.Error)
	setValue(v *credentials.Value)
	setExpiration(exp time.Time)
}

// retrieve gets sts.Credentials, converts them into credentials.Value, and
// updates provider's expiration time. Provider has the option of storing a
// permanent error to avoid making unnecessary API calls in a provider chain.
func retrieve(p stsCredsProvider) (credentials.Value, error) {
	v := credentials.Value{ProviderName: p.name()}
	err := p.getError()
	if err == nil {
		var c *sts.Credentials
		if c, err = p.getCreds(); err == nil {
			v.AccessKeyID = *c.AccessKeyId
			v.SecretAccessKey = *c.SecretAccessKey
			v.SessionToken = *c.SessionToken
			p.setExpiration(c.Expiration.Add(-time.Minute))
		} else if e, ok := err.(awserr.Error); ok {
			p.setError(e)
			p.setExpiration(time.Time{})
		}
	}
	p.setValue(&v)
	return v, err
}

// stsCreds partially implements CredsProvider and stsCredsProvider interfaces.
type stsCreds struct {
	val credentials.Value
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

// Save returns a static copy of the most recently retrieved credentials.
func (c *stsCreds) Save(errExp time.Time) *StaticCreds {
	var s *StaticCreds
	if c.err != nil {
		s = &StaticCreds{Err: c.err, Exp: errExp}
	} else if c.val.AccessKeyID != "" {
		s = &StaticCreds{Value: c.val, Exp: c.exp}
	} else {
		return nil
	}
	return s.Save(errExp)
}

func (c *stsCreds) getError() error               { return c.err }
func (c *stsCreds) setError(err awserr.Error)     { c.err = err }
func (c *stsCreds) setValue(v *credentials.Value) { c.val = *v }
func (c *stsCreds) setExpiration(exp time.Time)   { c.exp = exp }
