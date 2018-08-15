package creds

import (
	"errors"
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
	p := NewProxy(&cfg)
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

func TestZeroProvider(t *testing.T) {
	var p Provider
	cr, err := p.Creds()
	assert.NoError(t, err)
	assert.Zero(t, cr)

	cr, err = p.Retrieve()
	assert.Equal(t, ErrUnable, err)
	assert.Zero(t, cr)

	want := aws.Credentials{Source: "external"}
	p.Store(want, nil)
	cr, err = p.Retrieve()
	assert.NoError(t, err)
	assert.Equal(t, want, cr)
}

func TestStaticProvider(t *testing.T) {
	fast.MockTime(fast.Time())
	defer fast.MockTime(time.Time{})

	want := aws.Credentials{
		AccessKeyID:     "key",
		SecretAccessKey: "secret",
		SessionToken:    "token",
		CanExpire:       true,
		Expires:         fast.Time().Add(time.Hour),
	}
	p := StaticProvider(want, nil)
	cr, err := p.Retrieve()
	assert.NoError(t, err)
	want.Source = StaticProviderName
	assert.Equal(t, want, cr)

	fast.MockTime(fast.Time().Add(time.Hour - time.Minute))
	cr, err = p.Retrieve()
	assert.Equal(t, ErrUnable, err)
	assert.Equal(t, want, cr)

	want = aws.Credentials{Source: "BadCreds"}
	e := errors.New("bad creds")
	p = StaticProvider(want, e)
	cr, err = p.Retrieve()
	assert.Equal(t, e, err)
	assert.Equal(t, want, cr)
}

func TestWrap(t *testing.T) {
	static := aws.NewStaticCredentialsProvider("key", "secret", "")
	static.Value.Source = "test"

	cr, err := Wrap(static).Retrieve()
	assert.NoError(t, err)
	assert.Equal(t, static.Value, cr)

	cr, err = Wrap(Wrap(&static)).Retrieve()
	assert.NoError(t, err)
	assert.Equal(t, static.Value, cr)

	safe := aws.SafeCredentialsProvider{RetrieveFn: func() (aws.Credentials, error) {
		return static.Value, nil
	}}
	cr, err = Wrap(&safe).Retrieve()
	assert.NoError(t, err)
	assert.Equal(t, static.Value, cr)

	cached, _ := safe.Retrieve()
	safe.RetrieveFn = func() (aws.Credentials, error) { panic("fail") }
	cr, err = Wrap(&safe).Retrieve()
	assert.NoError(t, err)
	assert.Equal(t, cached, cr)
}

func TestProvider(t *testing.T) {
	fast.MockTime(fast.Time())
	defer fast.MockTime(time.Time{})

	// Permanent creds
	calls := 0
	p := RenewableProvider(func() (aws.Credentials, error) {
		calls++
		return aws.Credentials{AccessKeyID: "key1"}, nil
	})
	cr, err := p.Retrieve()
	assert.Equal(t, 1, calls)
	assert.NoError(t, err)
	assert.Equal(t, aws.Credentials{AccessKeyID: "key1"}, cr)

	assert.NoError(t, p.Ensure(24*time.Hour))
	assert.Equal(t, 1, calls)

	assert.NoError(t, p.Ensure(-1))
	assert.Equal(t, 2, calls)

	cr, err = p.Creds()
	assert.NoError(t, err)
	assert.Equal(t, aws.Credentials{AccessKeyID: "key1"}, cr)

	// Temporary creds
	calls = 0
	p = RenewableProvider(func() (aws.Credentials, error) {
		calls++
		return aws.Credentials{
			AccessKeyID: "key2",
			CanExpire:   true,
			Expires:     fast.Time().Add(time.Hour),
		}, nil
	})
	cr, err = p.Creds()
	assert.Equal(t, 0, calls)
	assert.NoError(t, err)
	assert.Equal(t, aws.Credentials{}, cr)

	assert.NoError(t, p.Ensure(time.Hour-1))
	assert.Equal(t, 1, calls)

	cr, err = p.Retrieve()
	assert.Equal(t, 1, calls)
	assert.NoError(t, err)
	want := aws.Credentials{
		AccessKeyID: "key2",
		CanExpire:   true,
		Expires:     fast.Time().Add(time.Hour),
	}
	assert.Equal(t, want, cr)

	// Renewal
	fast.MockTime(fast.Time().Add(30 * time.Minute))
	assert.NoError(t, p.Ensure(30*time.Minute))
	assert.Equal(t, 2, calls)

	cr, err = p.Creds()
	assert.NoError(t, err)
	want.Expires = fast.Time().Add(time.Hour)
	assert.Equal(t, want, cr)

	// Invalid duration
	assert.Equal(t, ErrUnable, p.Ensure(time.Hour))
	assert.Equal(t, 3, calls)

	assert.NoError(t, p.Ensure(30*time.Minute))
	assert.Equal(t, 3, calls)

	cr, err = p.Creds()
	assert.NoError(t, err)
	assert.Equal(t, want, cr)

	// Error caching
	e := errors.New("temporary error")
	calls = 0
	p = RenewableProvider(func() (cr aws.Credentials, err error) {
		if calls++; calls == 1 {
			err = e
		}
		return
	})
	cr, err = p.Retrieve()
	assert.Equal(t, 1, calls)
	assert.Equal(t, e, err)
	want = aws.Credentials{
		CanExpire: true,
		Expires:   fast.Time().Add(2 * time.Hour),
	}
	assert.Equal(t, want, cr)

	assert.Equal(t, e, p.Ensure(time.Hour))
	assert.Equal(t, 1, calls)

	fast.MockTime(fast.Time().Add(2*time.Hour - 1))
	assert.Equal(t, e, p.Ensure(24*time.Hour))
	assert.Equal(t, 1, calls)

	fast.MockTime(fast.Time().Add(1))
	assert.NoError(t, p.Ensure(0))
	assert.Equal(t, 2, calls)

	cr, _ = p.Creds()
	assert.Equal(t, aws.Credentials{}, cr)
}
