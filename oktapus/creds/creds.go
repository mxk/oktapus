package creds

import (
	"errors"
	"path"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/LuminalHQ/cloudcover/x/arn"
	"github.com/LuminalHQ/cloudcover/x/fast"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sts"
)

// Source names of credentials providers.
const (
	ProxyProviderName  = "ProxyCredentialsProvider"
	StaticProviderName = "StaticCredentialsProvider"
)

// ErrUnable is returned by Provider if the credentials do not satisfy the
// requested validity duration after successful renewal.
var ErrUnable = errors.New("creds: unable to satisfy minimum expiration time")

// FromSTS converts STS credentials to client credentials.
func FromSTS(src *sts.Credentials) aws.Credentials {
	if src == nil {
		return aws.Credentials{}
	}
	return aws.Credentials{
		AccessKeyID:     aws.StringValue(src.AccessKeyId),
		SecretAccessKey: aws.StringValue(src.SecretAccessKey),
		SessionToken:    aws.StringValue(src.SessionToken),
		Source:          "STS",
		CanExpire:       true,
		Expires:         aws.TimeValue(src.Expiration),
	}
}

// Set is a convenience function to set client credentials. SDK v2 is a bit
// confused about which field to use for this purpose.
func Set(c *aws.Client, cp aws.CredentialsProvider) {
	c.Credentials = cp
	c.Config.Credentials = cp
}

// Ident contains the results of sts:GetCallerIdentity API call.
type Ident struct {
	arn.ARN
	Account string
	UserID  string
}

// Set updates identity information from call output.
func (id *Ident) Set(out *sts.GetCallerIdentityOutput) {
	id.ARN = arn.Value(out.Arn)
	id.Account = aws.StringValue(out.Account)
	id.UserID = aws.StringValue(out.UserId)
}

// SessName returns the RoleSessionName for the current identity.
func (id Ident) SessName() string {
	if i := strings.IndexByte(id.UserID, ':'); i != -1 {
		return id.UserID[i+1:] // Current RoleSessionName or EC2 instance ID
	}
	if id.Type() == "user" {
		return id.Name()
	}
	return id.UserID
}

// Proxy provides IAM role credentials via sts:AssumeRole API.
type Proxy struct {
	Client   sts.STS
	Ident    Ident
	SessName string
}

// NewProxy returns a new credentials proxy.
func NewProxy(cfg *aws.Config) *Proxy {
	return &Proxy{Client: *sts.New(*cfg)}
}

// Init initializes client identity information and role session name.
func (p *Proxy) Init() error {
	out, err := p.Client.GetCallerIdentityRequest(nil).Send()
	if err == nil {
		p.Ident.Set(out)
		p.SessName = p.Ident.SessName()
	}
	return err
}

// Role returns the ARN for the specified account and role name. Account may be
// empty to use the account of the client credentials.
func (p *Proxy) Role(account, role string) arn.ARN {
	if account == "" {
		account = p.Ident.Account
	}
	return arn.New(p.Ident.Partition(), "iam", "", account, "role",
		path.Clean("/"+role))
}

// AssumeRole returns a new Provider for the specified role. Default session
// duration is used if d is zero.
func (p *Proxy) AssumeRole(role arn.ARN, d time.Duration) *Provider {
	in := &sts.AssumeRoleInput{
		RoleArn:         arn.String(role),
		RoleSessionName: aws.String(p.SessName),
	}
	if d != 0 {
		in.DurationSeconds = aws.Int64(int64(d.Round(time.Second).Seconds()))
	}
	return p.Provider(in)
}

// Provider returns a new Provider that calls AssumeRole with the specified
// input.
func (p *Proxy) Provider(in *sts.AssumeRoleInput) *Provider {
	return RenewableProvider(func() (cr aws.Credentials, err error) {
		out, err := p.Client.AssumeRoleRequest(in).Send()
		if err == nil {
			cr = FromSTS(out.Credentials)
		}
		cr.Source = ProxyProviderName
		return
	})
}

// RenewFunc renews client credentials. CanExpire and Expires fields control
// error caching if an error is returned. If CanExpire is false, Provider
// automatically caches the error for a limited amount of time.
type RenewFunc func() (aws.Credentials, error)

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

// Wrap converts an existing aws.CredentialsProvider to a Provider instance. If
// cp is a SafeCredentialsProvider, it must not be used by other goroutines
// during this call, and its RetrieveFn will no longer be protected by a single
// mutex if the new and old providers are used concurrently.
func Wrap(cp aws.CredentialsProvider) *Provider {
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
		return cr.Credentials, cr.err
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
	if cr.keepCurrent(d) || cr.err != nil {
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
