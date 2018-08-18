package creds

import (
	"testing"
	"time"

	"github.com/LuminalHQ/cloudcover/x/awsmock"
	"github.com/LuminalHQ/cloudcover/x/fast"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSessName(t *testing.T) {
	assert.Equal(t, "y",
		Ident{UserID: "x:y"}.SessName())
	assert.Equal(t, "alice",
		Ident{ARN: "arn:aws:iam::000000000000:user/alice"}.SessName())
	assert.Equal(t, "000000000001",
		Ident{ARN: "arn:aws:iam::000000000000:root", UserID: "000000000001"}.SessName())
}

func TestProxy(t *testing.T) {
	now := fast.Time()
	cfg := awsmock.Config(func(r *aws.Request) {
		switch out := r.Data.(type) {
		case *sts.GetCallerIdentityOutput:
			out.Account = aws.String("000000000000")
			out.Arn = aws.String("arn:aws:iam::000000000000:user/alice")
		case *sts.AssumeRoleOutput:
			want := &sts.AssumeRoleInput{
				RoleArn:         aws.String("arn:aws:iam::000000000000:role/testrole"),
				RoleSessionName: aws.String("alice"),
			}
			assert.Equal(t, want, r.Params)
			out.Credentials = &sts.Credentials{
				AccessKeyId:     aws.String("tempkey"),
				SecretAccessKey: aws.String("secret"),
				SessionToken:    aws.String("token"),
				Expiration:      aws.Time(now.Add(time.Hour)),
			}
		}
	})
	p := Proxy{Client: NewClient(&cfg)}
	require.NoError(t, p.Init())

	cr, err := p.AssumeRole(p.Role("", "testrole"), 0).Retrieve()
	require.NoError(t, err)
	want := aws.Credentials{
		AccessKeyID:     "tempkey",
		SecretAccessKey: "secret",
		SessionToken:    "token",
		Source:          ProxyProviderName,
		CanExpire:       true,
		Expires:         now.Add(time.Hour),
	}
	assert.Equal(t, want, cr)
}
