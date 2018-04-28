package awsx

import (
	"errors"
	"time"

	"github.com/LuminalHQ/cloudcover/oktapus/internal"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/service/sts"
)

// ErrCredsExpired indicates that static credentials are no longer valid.
var ErrCredsExpired = errors.New("awsx: credentials have expired")

// AssumeRoleWithSAMLFunc is the signature of sts:AssumeRoleWithSAML API call.
type AssumeRoleWithSAMLFunc func(*sts.AssumeRoleWithSAMLInput) (*sts.AssumeRoleWithSAMLOutput, error)

// AssumeRoleFunc is the signature of sts:AssumeRole API call.
type AssumeRoleFunc func(*sts.AssumeRoleInput) (*sts.AssumeRoleOutput, error)

// CredsProvider replaces credentials.Provider interface. It allows credentials
// to be saved and reused across process invocations.
type CredsProvider interface {
	Creds() *credentials.Credentials
	Expires() time.Time
	Save() *StaticCreds
	Reset()

	mustRetrieve() bool
	retrieve() (credentials.Value, error)
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
		c := &SavedCreds{*saved, next}
		c.saved.creds = nil
		return c
	}
	return next
}

// Creds returns credentials using SavedCreds as the provider.
func (c *SavedCreds) Creds() *credentials.Credentials {
	if c.saved.creds == nil {
		c.saved.creds = credentials.NewCredentials(provider{c})
	}
	return c.saved.creds
}

// Expires returns the expiration time of the current provider.
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

// mustRetrieve returns true when retrieve() must be called.
func (c *SavedCreds) mustRetrieve() bool {
	if c.saved.valid() {
		return c.saved.Err != nil
	}
	return c.next.mustRetrieve()
}

// retrieve returns credentials from the first available provider.
func (c *SavedCreds) retrieve() (credentials.Value, error) {
	if c.saved.Err != ErrCredsExpired {
		if v, err := c.saved.retrieve(); err != ErrCredsExpired {
			return v, err
		}
	}
	return c.next.retrieve()
}

// StaticCreds is a CredsProvider that can be encoded for external storage. It
// returns either valid credentials or an error until the expiration time. After
// that, it returns ErrCredsExpired.
type StaticCreds struct {
	credentials.Value
	Err error
	Exp time.Time

	creds *credentials.Credentials
}

// NewStaticCreds returns static credentials that do not expire.
func NewStaticCreds(accessKeyID, secretAccessKey, sessionToken string) *StaticCreds {
	return &StaticCreds{
		Value: credentials.Value{
			AccessKeyID:     accessKeyID,
			SecretAccessKey: secretAccessKey,
			SessionToken:    sessionToken,
		},
		Exp: time.Date(9999, 1, 1, 0, 0, 0, 0, time.UTC),
	}
}

// Creds returns credentials using StaticCreds as the provider.
func (c *StaticCreds) Creds() *credentials.Credentials {
	if c.creds == nil {
		c.creds = credentials.NewCredentials(provider{c})
	}
	return c.creds
}

// Expires returns the expiration time.
func (c *StaticCreds) Expires() time.Time {
	return c.Exp
}

// Save returns a copy of c if it hasn't expired or nil otherwise.
func (c *StaticCreds) Save() *StaticCreds {
	if !c.valid() {
		return nil
	}
	s := *c
	s.ProviderName = ""
	s.Err = internal.RegisteredError(c.Err)
	s.creds = nil
	return &s
}

// Reset forces any cached credentials to expire.
func (c *StaticCreds) Reset() {
	c.Exp = time.Time{}
}

// mustRetrieve returns true when retrieve() must be called.
func (c *StaticCreds) mustRetrieve() bool {
	return c.Err != nil || !c.valid()
}

// retrieve returns static credentials or an error until their expiration.
func (c *StaticCreds) retrieve() (credentials.Value, error) {
	c.valid()
	c.ProviderName = "StaticCreds"
	return c.Value, c.Err
}

// valid returns true until c expires.
func (c *StaticCreds) valid() bool {
	if c.Err != ErrCredsExpired {
		if !c.Exp.IsZero() && c.Exp.After(internal.Time()) {
			return true
		}
		*c = StaticCreds{Err: ErrCredsExpired, creds: c.creds}
	}
	return false
}

// SAMLCreds is a CredsProvider that exchanges a SAML assertion for temporary
// security credentials. If Renew is set, it is called to renew the SAML
// assertion before calling AssumeRoleWithSAML.
type SAMLCreds struct {
	sts.AssumeRoleWithSAMLInput
	Renew func(in *sts.AssumeRoleWithSAMLInput) error

	stsCreds
	api AssumeRoleWithSAMLFunc
}

// NewSAMLCreds returns a new SAML-based CredsProvider.
func NewSAMLCreds(api AssumeRoleWithSAMLFunc, principal, role ARN, saml string) *SAMLCreds {
	return &SAMLCreds{
		AssumeRoleWithSAMLInput: sts.AssumeRoleWithSAMLInput{
			PrincipalArn:  principal.Str(),
			RoleArn:       role.Str(),
			SAMLAssertion: aws.String(saml),
		},
		api: api,
	}
}

// Creds returns credentials using SAMLCreds as the provider.
func (c *SAMLCreds) Creds() *credentials.Credentials {
	if c.s.creds == nil {
		c.s.creds = credentials.NewCredentials(provider{c})
	}
	return c.s.creds
}

// retrieve returns new STS credentials.
func (c *SAMLCreds) retrieve() (credentials.Value, error) {
	return c.stsRetrieve("SAMLCreds", func() (*sts.Credentials, error) {
		if c.Renew != nil {
			if err := c.Renew(&c.AssumeRoleWithSAMLInput); err != nil {
				return nil, err
			}
		}
		out, err := c.api(&c.AssumeRoleWithSAMLInput)
		return out.Credentials, err
	})
}

// AssumeRoleCreds is a CredsProvider that calls sts:AssumeRole to get temporary
// security credentials.
type AssumeRoleCreds struct {
	sts.AssumeRoleInput

	stsCreds
	api AssumeRoleFunc
}

// NewAssumeRoleCreds returns a new role-based CredsProvider.
func NewAssumeRoleCreds(api AssumeRoleFunc, role ARN, roleSessionName string) *AssumeRoleCreds {
	return &AssumeRoleCreds{
		AssumeRoleInput: sts.AssumeRoleInput{
			RoleArn:         role.Str(),
			RoleSessionName: aws.String(roleSessionName),
		},
		api: api,
	}
}

// Creds returns credentials using AssumeRoleCreds as the provider.
func (c *AssumeRoleCreds) Creds() *credentials.Credentials {
	if c.s.creds == nil {
		c.s.creds = credentials.NewCredentials(provider{c})
	}
	return c.s.creds
}

// retrieve returns new STS credentials.
func (c *AssumeRoleCreds) retrieve() (credentials.Value, error) {
	return c.stsRetrieve("AssumeRoleCreds", func() (*sts.Credentials, error) {
		out, err := c.api(&c.AssumeRoleInput)
		return out.Credentials, err
	})
}

// stsCreds is a common base for STS credential providers. StaticCreds are used
// for convenience and should not be accessed directly.
type stsCreds struct{ s StaticCreds }

// CredsProvider interface.
func (c *stsCreds) Expires() time.Time { return c.s.Expires() }
func (c *stsCreds) Save() *StaticCreds { return c.s.Save() }
func (c *stsCreds) Reset()             { c.s.Reset() }
func (c *stsCreds) mustRetrieve() bool { return c.s.mustRetrieve() }

// retrieve gets sts.Credentials from fn, converts them into credentials.Value,
// and updates the expiration time.
func (c *stsCreds) stsRetrieve(name string, fn func() (*sts.Credentials, error)) (credentials.Value, error) {
	if c.s.valid() {
		return c.s.Value, c.s.Err // Unexpired error
	}
	if creds, err := fn(); err == nil {
		c.s.Value = credentials.Value{
			AccessKeyID:     *creds.AccessKeyId,
			SecretAccessKey: *creds.SecretAccessKey,
			SessionToken:    *creds.SessionToken,
			ProviderName:    name,
		}
		c.s.Err = nil
		c.s.Exp = creds.Expiration.Add(-time.Minute).Truncate(time.Second)
	} else {
		// TODO: Error expiration time should be reduced for temporary errors
		c.s.Value = credentials.Value{ProviderName: name}
		c.s.Err = err
		c.s.Exp = internal.Time().Add(2 * time.Hour).Truncate(time.Second)
	}
	return c.s.Value, c.s.Err
}

// provider implements credentials.Provider interface.
type provider struct{ CredsProvider }

func (p provider) Retrieve() (credentials.Value, error) { return p.retrieve() }
func (p provider) IsExpired() bool                      { return p.mustRetrieve() }
