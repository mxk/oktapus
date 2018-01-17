package awsgw

import (
	"errors"
	"time"

	"github.com/LuminalHQ/oktapus/internal"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/service/sts"
)

// ErrCredsExpired indicates that static credentials are no longer valid.
var ErrCredsExpired = errors.New("awsgw: credentials have expired")

// AssumeRoleWithSAMLFunc is the signature of sts:AssumeRoleWithSAML API call.
type AssumeRoleWithSAMLFunc func(*sts.AssumeRoleWithSAMLInput) (*sts.AssumeRoleWithSAMLOutput, error)

// AssumeRoleFunc is the signature of sts:AssumeRole API call.
type AssumeRoleFunc func(*sts.AssumeRoleInput) (*sts.AssumeRoleOutput, error)

// CredsProvider extends credentials.Provider interface. It allows credentials
// to be saved and reused across process invocations. Concurrency guarantees
// from credentials.Credentials do not extend to these additional methods.
type CredsProvider interface {
	credentials.Provider
	Expires() time.Time
	Save() *StaticCreds
	Reset()
}

// SavedCreds is a CredsProvider that uses static credentials until they expire
// and then switches to the next CredsProvider.
type SavedCreds struct {
	saved StaticCreds
	next  CredsProvider
}

// NewSavedCreds combines StaticCreds, if any, with another CredsProvider.
func NewSavedCreds(saved *StaticCreds, next CredsProvider) CredsProvider {
	if saved != nil && saved.valid() {
		return &SavedCreds{*saved, next}
	}
	return next
}

// Retrieve returns credentials from the first available provider.
func (c *SavedCreds) Retrieve() (credentials.Value, error) {
	if c.saved.Err != ErrCredsExpired {
		if v, err := c.saved.Retrieve(); err != ErrCredsExpired {
			return v, err
		}
	}
	return c.next.Retrieve()
}

// IsExpired determines whether Retrieve() should be called. It does not return
// actual expiration status and should not be used by anything other than
// credentials.Credentials.
func (c *SavedCreds) IsExpired() bool {
	if c.saved.valid() {
		return c.saved.Err != nil
	}
	return c.next.IsExpired()
}

// Expires returns the actual expiration time of the current provider.
func (c *SavedCreds) Expires() time.Time {
	if c.saved.valid() {
		return c.saved.Exp
	}
	return c.next.Expires()
}

// Save returns a static copy of the most recently retrieved credentials or nil
// if none of the cached credentials are valid.
func (c *SavedCreds) Save() *StaticCreds {
	if s := c.saved.Save(); s != nil {
		return s
	}
	return c.next.Save()
}

// Reset forces any cached credentials to expire.
func (c *SavedCreds) Reset() {
	c.saved.Reset()
	c.next.Reset()
}

// StaticCreds is a CredsProvider that can be encoded for external storage. It
// returns either valid credentials or an error until the expiration time. After
// that, it returns ErrCredsExpired. Use SavedCreds instead of ChainProvider to
// get correct error behavior in combination with other providers.
type StaticCreds struct {
	credentials.Value
	Err error
	Exp time.Time
}

// Retrieve returns static credentials or an error until their expiration.
func (c *StaticCreds) Retrieve() (credentials.Value, error) {
	c.valid()
	c.ProviderName = "StaticCreds"
	return c.Value, c.Err
}

// IsExpired determines whether Retrieve() should be called. It does not return
// actual expiration status and should not be used by anything other than
// credentials.Credentials.
func (c *StaticCreds) IsExpired() bool {
	return c.Err != nil || !c.valid()
}

// Expires returns the actual expiration time.
func (c *StaticCreds) Expires() time.Time {
	return c.Exp
}

// Save returns a copy of c if it hasn't expired or nil otherwise.
func (c *StaticCreds) Save() *StaticCreds {
	if c.valid() {
		s := *c
		s.ProviderName = ""
		s.Err = encodableError(c.Err)
		return &s
	}
	return nil
}

// Reset forces any cached credentials to expire.
func (c *StaticCreds) Reset() {
	c.Exp = time.Time{}
}

// valid returns true until c expires.
func (c *StaticCreds) valid() bool {
	if c.Err != ErrCredsExpired {
		if !c.Exp.IsZero() && c.Exp.After(internal.Time()) {
			return true
		}
		*c = StaticCreds{Err: ErrCredsExpired}
	}
	return false
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

// Retrieve returns new STS credentials.
func (c *SAMLCreds) Retrieve() (credentials.Value, error) {
	return c.retrieve("SAMLCreds", func() (*sts.Credentials, error) {
		out, err := c.api(&c.AssumeRoleWithSAMLInput)
		if out != nil {
			return out.Credentials, err
		}
		return nil, err
	})
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

// Retrieve returns new STS credentials.
func (c *AssumeRoleCreds) Retrieve() (credentials.Value, error) {
	return c.retrieve("AssumeRoleCreds", func() (*sts.Credentials, error) {
		out, err := c.api(&c.AssumeRoleInput)
		if out != nil {
			return out.Credentials, err
		}
		return nil, err
	})
}

// stsCreds is a common base for STS credential providers. StaticCreds are used
// for convenience and should not be accessed directly.
type stsCreds struct{ s StaticCreds }

// CredsProvider interface.
func (c *stsCreds) IsExpired() bool    { return c.s.IsExpired() }
func (c *stsCreds) Expires() time.Time { return c.s.Expires() }
func (c *stsCreds) Save() *StaticCreds { return c.s.Save() }
func (c *stsCreds) Reset()             { c.s.Reset() }

// retrieve gets sts.Credentials from fn, converts them into credentials.Value,
// and updates the expiration time.
func (c *stsCreds) retrieve(name string, fn func() (*sts.Credentials, error)) (credentials.Value, error) {
	if c.s.valid() {
		return c.s.Value, c.s.Err // Unexpired error
	}
	if creds, err := fn(); err == nil {
		*c = stsCreds{StaticCreds{
			Value: credentials.Value{
				AccessKeyID:     *creds.AccessKeyId,
				SecretAccessKey: *creds.SecretAccessKey,
				SessionToken:    *creds.SessionToken,
				ProviderName:    name,
			},
			Exp: creds.Expiration.Add(-45 * time.Second).Truncate(time.Minute),
		}}
	} else {
		// TODO: Error expiration time should be reduced for temporary errors
		*c = stsCreds{StaticCreds{
			Value: credentials.Value{ProviderName: name},
			Err:   err,
			Exp:   internal.Time().Add(2 * time.Hour).Truncate(time.Minute),
		}}
	}
	return c.s.Value, c.s.Err
}
