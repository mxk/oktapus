package creds

import (
	"errors"
	"testing"
	"time"

	"github.com/LuminalHQ/cloudcover/x/fast"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/stretchr/testify/assert"
)

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

	cr, err := WrapProvider(static).Retrieve()
	assert.NoError(t, err)
	assert.Equal(t, static.Value, cr)

	cr, err = WrapProvider(WrapProvider(&static)).Retrieve()
	assert.NoError(t, err)
	assert.Equal(t, static.Value, cr)

	safe := aws.SafeCredentialsProvider{RetrieveFn: func() (aws.Credentials, error) {
		return static.Value, nil
	}}
	cr, err = WrapProvider(&safe).Retrieve()
	assert.NoError(t, err)
	assert.Equal(t, static.Value, cr)

	cached, _ := safe.Retrieve()
	safe.RetrieveFn = func() (aws.Credentials, error) { panic("fail") }
	cr, err = WrapProvider(&safe).Retrieve()
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
