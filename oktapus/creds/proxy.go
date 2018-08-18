package creds

import (
	"path"
	"strings"
	"time"

	"github.com/LuminalHQ/cloudcover/x/arn"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sts"
)

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

// Client extends STS API client.
type Client struct{ sts.STS }

// NewClient returns a new STS client.
func NewClient(cfg *aws.Config) Client { return Client{*sts.New(*cfg)} }

// GobEncode prevents the client from being encoded by gob.
func (Client) GobEncode() ([]byte, error) { return nil, nil }

// GobDecode prevents the client from being decoded by gob.
func (Client) GobDecode([]byte) error { return nil }

// Proxy provides IAM role credentials via sts:AssumeRole API.
type Proxy struct {
	Client   Client
	Ident    Ident
	SessName string
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
