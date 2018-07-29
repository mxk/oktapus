package creds

import (
	"errors"
	"path"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/LuminalHQ/cloudcover/x/arn"
	"github.com/LuminalHQ/cloudcover/x/fast"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sts"
)

// ProxyProvider is the source name of Proxy credentials.
const ProxyProvider = "ProxyCredentialsProvider"

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

// NewProxy creates a new credentials proxy.
func NewProxy(cfg *aws.Config) (*Proxy, error) {
	p := &Proxy{Client: *sts.New(*cfg)}
	out, err := p.Client.GetCallerIdentityRequest(nil).Send()
	if err != nil {
		return nil, err
	}
	p.Ident.Set(out)
	p.SessName = p.Ident.SessName()
	return p, nil
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
		cr.Source = ProxyProvider
		return
	})
}

// RenewFunc renews client credentials. CanExpire and Expires fields control
// error caching if an error is returned. If CanExpire is false, Provider
// automatically caches the error for a limited amount of time.
type RenewFunc func() (aws.Credentials, error)

// Provider is a replacement for aws.SafeCredentialsProvider. It allows clients
// to ensure credential validity for a period of time in the future, and caches
// error information to avoid unnecessary network traffic. Provider values must
// not be copied.
type Provider struct {
	cr    atomic.Value
	mu    sync.Mutex
	renew RenewFunc
}

// RenewableProvider returns an aws.CredentialsProvider that automatically
// renews its credentials as they expire.
func RenewableProvider(fn RenewFunc) *Provider {
	p := &Provider{renew: fn}
	p.cr.Store((*creds)(nil))
	return p
}

// Retrieve implements aws.CredentialsProvider.
func (p *Provider) Retrieve() (aws.Credentials, error) {
	return p.ensure(time.Minute)
}

// Creds returns currently cached credentials and error without renewal.
func (p *Provider) Creds() (aws.Credentials, error) {
	if cr := p.cr.Load().(*creds); cr != nil {
		return cr.Credentials, cr.err
	}
	return aws.Credentials{}, nil
}

// Ensure ensures that credentials will remain valid for the specified duration,
// renewing them if necessary. A negative duration forces unconditional renewal.
// ErrUnable is returned if the validity period cannot be satisfied.
func (p *Provider) Ensure(d time.Duration) error {
	_, err := p.ensure(d)
	return err
}

func (p *Provider) ensure(d time.Duration) (aws.Credentials, error) {
	cr := p.cr.Load().(*creds)
	if cr.keepCurrent(d) {
		return cr.Credentials, cr.err
	}
	if p.renew != nil {
		p.mu.Lock()
		defer p.mu.Unlock()
		if cr = p.cr.Load().(*creds); cr.keepCurrent(d) {
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
