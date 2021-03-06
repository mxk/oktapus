package creds

import (
	"errors"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/ec2rolecreds"
	"github.com/mxk/go-fast"
	"github.com/stretchr/testify/assert"
)

func TestValid(t *testing.T) {
	fast.MockTime(fast.Time())
	defer fast.MockTime(time.Time{})

	assert.False(t, ValidFor(nil, 0))

	// No creds
	cr := aws.Credentials{}
	cr.Expires = fast.Time().Add(time.Minute)
	assert.False(t, ValidFor(&cr, time.Hour))

	// Permanent creds
	cr.AccessKeyID = "id"
	cr.SecretAccessKey = "secret"
	assert.True(t, ValidFor(&cr, time.Hour))
	assert.False(t, ValidFor(&cr, -1))

	// Temporary creds
	cr.CanExpire = true
	assert.True(t, ValidUntil(&cr, cr.Expires))
	assert.False(t, ValidUntil(&cr, cr.Expires.Add(1)))
	assert.True(t, ValidFor(&cr, time.Minute))
	assert.False(t, ValidFor(&cr, time.Minute+1))
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

	ec2cr := ec2rolecreds.NewProvider(nil)
	assert.NotPanics(t, func() { WrapProvider(ec2cr) })

	cached, _ := safe.Retrieve()
	safe.RetrieveFn = func() (aws.Credentials, error) { panic("fail") }
	cr, err = WrapProvider(&safe).Retrieve()
	assert.NoError(t, err)
	assert.Equal(t, cached, cr)
}

func TestPermanent(t *testing.T) {
	fast.MockTime(fast.Time())
	defer fast.MockTime(time.Time{})

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
}

func TestTemporary(t *testing.T) {
	fast.MockTime(fast.Time())
	defer fast.MockTime(time.Time{})

	calls := 0
	p := RenewableProvider(func() (aws.Credentials, error) {
		calls++
		return aws.Credentials{
			AccessKeyID: "key2",
			CanExpire:   true,
			Expires:     fast.Time().Add(time.Hour),
		}, nil
	})
	cr, err := p.Creds()
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

	// Renew
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
}

func TestError(t *testing.T) {
	fast.MockTime(fast.Time())
	defer fast.MockTime(time.Time{})

	e := errors.New("temporary error")
	calls := 0
	p := RenewableProvider(func() (cr aws.Credentials, err error) {
		if calls++; calls == 1 {
			err = e
		}
		return
	})
	cr, err := p.Retrieve()
	assert.Equal(t, 1, calls)
	assert.Equal(t, e, err)
	want := aws.Credentials{
		CanExpire: true,
		Expires:   fast.Time().Add(2 * time.Hour),
	}
	assert.Equal(t, want, cr)

	assert.Equal(t, e, p.Ensure(time.Hour))
	assert.Equal(t, 1, calls)

	fast.MockTime(fast.Time().Add(2*time.Hour - 1))
	cr, err = p.Creds()
	assert.Equal(t, e, err)
	assert.Equal(t, want, cr)
	assert.Equal(t, e, p.Ensure(24*time.Hour))
	assert.Equal(t, 1, calls)

	fast.MockTime(fast.Time().Add(1))
	cr, err = p.Creds()
	assert.NoError(t, err)
	assert.Equal(t, want, cr)
	assert.NoError(t, p.Ensure(0))
	assert.Equal(t, 2, calls)

	cr, _ = p.Creds()
	assert.Equal(t, aws.Credentials{}, cr)
}
