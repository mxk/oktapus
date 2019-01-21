package creds

import (
	"errors"
	"reflect"
	"sync"
	"sync/atomic"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/ec2rolecreds"
	"github.com/mxk/cloudcover/x/fast"
)

// Source names of credentials providers.
const (
	ProxyProviderName  = "ProxyCredentialsProvider"
	StaticProviderName = "StaticCredentialsProvider"
)

// ErrUnable is returned by Provider if the credentials do not satisfy the
// requested validity duration after successful renewal.
var ErrUnable = errors.New("creds: unable to satisfy minimum expiration time")

// RenewFunc renews client credentials. CanExpire and Expires fields control
// error caching if an error is returned. If CanExpire is false, Provider
// automatically caches the error for a limited amount of time.
type RenewFunc func() (aws.Credentials, error)

// ValidUntil returns true if credentials cr will remain valid until time t.
func ValidUntil(cr *aws.Credentials, t time.Time) bool {
	return cr != nil && cr.HasKeys() && !(cr.CanExpire && cr.Expires.Before(t))
}

// ValidFor returns true if credentials cr will remain valid for duration d.
func ValidFor(cr *aws.Credentials, d time.Duration) bool {
	return d >= 0 && ValidUntil(cr, fast.Time().Add(d))
}

// Provider is a replacement for aws.SafeCredentialsProvider. It allows clients
// to ensure credential validity for a period of time in the future. It also
// caches errors to avoid unnecessary network traffic. Provider values must not
// be copied.
type Provider struct {
	cr    atomic.Value
	mu    sync.Mutex
	renew RenewFunc
}

// StaticProvider returns an aws.CredentialsProvider that provides static
// credentials or an error.
func StaticProvider(cr aws.Credentials, err error) *Provider {
	if cr.Source == "" {
		cr.Source = StaticProviderName
	}
	p := new(Provider)
	p.Store(cr, err)
	return p
}

// RenewableProvider returns an aws.CredentialsProvider that automatically
// renews its credentials as they expire.
func RenewableProvider(fn RenewFunc) *Provider {
	return &Provider{renew: fn}
}

// WrapProvider converts an existing aws.CredentialsProvider to a Provider
// instance. If cp is a SafeCredentialsProvider, it must not be used by other
// goroutines during this call, and its RetrieveFn will no longer be protected
// by a single mutex if the old and new providers are used concurrently.
func WrapProvider(cp aws.CredentialsProvider) *Provider {
	switch cp := cp.(type) {
	case *Provider:
		return cp
	case aws.StaticCredentialsProvider:
		return StaticProvider(cp.Retrieve())
	case *aws.StaticCredentialsProvider:
		return StaticProvider(cp.Retrieve())
	case *aws.SafeCredentialsProvider:
		// Temporarily swap out RetrieveFn to get cached creds
		fn := cp.RetrieveFn
		cp.RetrieveFn = func() (aws.Credentials, error) {
			return aws.Credentials{}, ErrUnable
		}
		cr, err := cp.Retrieve()
		cp.RetrieveFn = fn
		p := RenewableProvider(fn)
		if err != ErrUnable {
			p.Store(cr, err)
		}
		return p
	case *ec2rolecreds.Provider:
		return WrapProvider(&cp.SafeCredentialsProvider)
	}
	// TODO: Handle ChainProvider?
	panic("creds: unsupported provider type " + reflect.TypeOf(cp).String())
}

// Retrieve implements aws.CredentialsProvider.
func (p *Provider) Retrieve() (aws.Credentials, error) {
	return p.ensure(time.Minute)
}

// Creds returns currently cached credentials and error without renewal.
func (p *Provider) Creds() (aws.Credentials, error) {
	if cr, _ := p.cr.Load().(*creds); cr != nil {
		err := cr.err
		if err != nil && cr.Credentials.CanExpire &&
			!cr.Credentials.Expires.After(fast.Time()) {
			err = nil
		}
		return cr.Credentials, err
	}
	return aws.Credentials{}, nil
}

// Store replaces any cached credentials and/or error with the specified values.
func (p *Provider) Store(cr aws.Credentials, err error) {
	p.cr.Store(&creds{cr, err})
}

// Ensure ensures that credentials will remain valid for the specified duration,
// renewing them if necessary. A negative duration forces unconditional renewal.
// ErrUnable is returned if the validity period cannot be satisfied.
func (p *Provider) Ensure(d time.Duration) error {
	_, err := p.ensure(d)
	return err
}

func (p *Provider) ensure(d time.Duration) (aws.Credentials, error) {
	cr, _ := p.cr.Load().(*creds)
	if cr.keepCurrent(d) {
		return cr.Credentials, cr.err
	}
	if p.renew != nil {
		p.mu.Lock()
		defer p.mu.Unlock()
		if cr, _ = p.cr.Load().(*creds); cr.keepCurrent(d) {
			return cr.Credentials, cr.err
		}
		cr = new(creds)
		cr.Credentials, cr.err = p.renew()
		if cr.err != nil && !cr.CanExpire {
			cr.CanExpire = true
			if t := fast.Time(); aws.IsErrorThrottle(cr.err) {
				cr.Expires = t.Add(2 * time.Minute)
			} else {
				cr.Expires = t.Add(2 * time.Hour)
			}
		}
		p.cr.Store(cr)
	} else if cr == nil {
		return aws.Credentials{}, ErrUnable
	}
	if d < 0 {
		d = 0
	}
	if cr.keepCurrent(d) {
		return cr.Credentials, cr.err
	}
	return cr.Credentials, ErrUnable
}

// creds extends aws.Credentials with a persistent error.
type creds struct {
	aws.Credentials
	err error
}

func (cr *creds) keepCurrent(d time.Duration) bool {
	if cr == nil || d < 0 {
		return false
	}
	if !cr.CanExpire {
		return true
	}
	t := fast.Time()
	return cr.Expires.After(t.Add(d)) || (cr.err != nil && cr.Expires.After(t))
}
