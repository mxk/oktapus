package awsx

import (
	"errors"
	"testing"
	"time"

	"github.com/LuminalHQ/cloudcover/x/fast"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/service/sts"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStaticCreds(t *testing.T) {
	exp := fast.MockTime(fast.Time()).Add(time.Minute)
	defer fast.MockTime(time.Time{})

	v := credsVal("StaticCreds")
	c := NewStaticCreds(v.AccessKeyID, v.SecretAccessKey, v.SessionToken)
	c.ProviderName = v.ProviderName
	require.Equal(t, v, c.Value)

	c.Exp = exp
	cr := c.Creds()
	require.NotNil(t, cr)

	// Valid
	rv, err := cr.Get()
	assert.NoError(t, err)
	assert.Equal(t, v, rv)
	assert.False(t, cr.IsExpired())
	assert.Equal(t, exp, c.Expires())

	// Save
	saved := c.Save()
	saved.ProviderName = c.ProviderName
	saved.creds = c.creds
	assert.Equal(t, c, saved)

	// Reset
	c.Reset()
	rv, err = cr.Get()
	assert.True(t, err == ErrCredsExpired)
	assert.Equal(t, credentials.Value{}, rv)
	assert.True(t, cr.IsExpired())
	assert.Zero(t, c.Expires())

	// Creds have not changed
	assert.True(t, c.Creds() == cr)
}

func TestStaticCredsExp(t *testing.T) {
	exp := fast.MockTime(fast.Time()).Add(time.Minute)
	defer fast.MockTime(time.Time{})

	v := credsVal("StaticCreds")
	c := &StaticCreds{Value: v, Exp: exp}
	cr := c.Creds()

	// Valid
	rv, err := c.retrieve()
	assert.NoError(t, err)
	assert.Equal(t, v, rv)

	// Expired
	fast.MockTime(exp)
	rv, err = c.retrieve()
	assert.True(t, err == ErrCredsExpired)
	assert.Equal(t, nilVal("StaticCreds"), rv)

	// Creds have not changed
	assert.True(t, c.Creds() == cr)
}

func TestSAMLCreds(t *testing.T) {
	fast.MockTime(fast.Time())
	defer fast.MockTime(time.Time{})

	api := func(in *sts.AssumeRoleWithSAMLInput) (*sts.AssumeRoleWithSAMLOutput, error) {
		assert.Equal(t, "principal", *in.PrincipalArn)
		assert.Equal(t, "role", *in.RoleArn)
		assert.Equal(t, "saml", *in.SAMLAssertion)
		return &sts.AssumeRoleWithSAMLOutput{
			Credentials: &sts.Credentials{
				AccessKeyId:     aws.String("ID"),
				Expiration:      aws.Time(fast.Time().Add(5 * time.Minute)),
				SecretAccessKey: aws.String("SECRET"),
				SessionToken:    aws.String("TOKEN"),
			},
		}, nil
	}
	c := NewSAMLCreds(api, "", "", "")
	c.Renew = func(in *sts.AssumeRoleWithSAMLInput) error {
		in.PrincipalArn = aws.String("principal")
		in.RoleArn = aws.String("role")
		in.SAMLAssertion = aws.String("saml")
		return nil
	}
	cr := c.Creds()
	require.NotNil(t, cr)

	// Renew
	v, err := cr.Get()
	assert.NoError(t, err)
	assert.Equal(t, credsVal("SAMLCreds"), v)
	assert.False(t, c.mustRetrieve())

	// Expire
	fast.MockTime(c.Expires())
	assert.True(t, c.mustRetrieve())

	// Renew
	v, err = cr.Get()
	assert.NoError(t, err)
	assert.Equal(t, credsVal("SAMLCreds"), v)
	assert.False(t, c.mustRetrieve())

	// Fail
	c.Reset()
	c.Renew = func(in *sts.AssumeRoleWithSAMLInput) error {
		return errors.New("renew error")
	}
	v, err = cr.Get()
	assert.EqualError(t, err, "renew error")
	assert.Equal(t, credentials.Value{}, v)
	assert.True(t, c.mustRetrieve())

	// Error is cached
	c.Renew = nil
	v, err = cr.Get()
	assert.EqualError(t, err, "renew error")

	// Creds have not changed
	assert.True(t, c.Creds() == cr)
}

func TestAssumeRoleCreds(t *testing.T) {
	fast.MockTime(fast.Time())
	defer fast.MockTime(time.Time{})

	api := func(in *sts.AssumeRoleInput) (*sts.AssumeRoleOutput, error) {
		assert.Equal(t, "role", *in.RoleArn)
		assert.Equal(t, "roleSessionName", *in.RoleSessionName)
		return &sts.AssumeRoleOutput{
			Credentials: &sts.Credentials{
				AccessKeyId:     aws.String("ID"),
				Expiration:      aws.Time(fast.Time().Add(5 * time.Minute)),
				SecretAccessKey: aws.String("SECRET"),
				SessionToken:    aws.String("TOKEN"),
			},
		}, nil
	}
	c := NewAssumeRoleCreds(api, "role", "roleSessionName")

	cr := c.Creds()
	require.NotNil(t, cr)

	// Renew
	v, err := cr.Get()
	assert.NoError(t, err)
	assert.Equal(t, credsVal("AssumeRoleCreds"), v)

	// Creds have not changed
	assert.True(t, c.Creds() == cr)
}

func TestAssumeRoleErr(t *testing.T) {
	fast.MockTime(fast.Time())
	defer fast.MockTime(time.Time{})

	nextErr := errors.New("call failed")
	api := func(in *sts.AssumeRoleInput) (*sts.AssumeRoleOutput, error) {
		if nextErr != nil {
			return new(sts.AssumeRoleOutput), nextErr
		}
		return &sts.AssumeRoleOutput{
			Credentials: &sts.Credentials{
				AccessKeyId:     aws.String("ID"),
				Expiration:      aws.Time(fast.Time().Add(5 * time.Minute)),
				SecretAccessKey: aws.String("SECRET"),
				SessionToken:    aws.String("TOKEN"),
			},
		}, nil
	}
	c := NewAssumeRoleCreds(api, "role", "roleSessionName")
	cr := c.Creds()

	// Fail
	v, err := c.retrieve()
	assert.True(t, err == nextErr)
	assert.Equal(t, nilVal("AssumeRoleCreds"), v)
	assert.True(t, c.mustRetrieve())

	// Error is cached
	cachedErr := nextErr
	nextErr = nil
	v, err = c.retrieve()
	assert.True(t, err == cachedErr)
	assert.Equal(t, nilVal("AssumeRoleCreds"), v)
	assert.True(t, c.mustRetrieve())

	// Error expired
	fast.MockTime(c.Expires())
	v, err = c.retrieve()
	assert.NoError(t, err)
	assert.Equal(t, credsVal("AssumeRoleCreds"), v)
	assert.False(t, c.mustRetrieve())

	// Reset
	c.Reset()
	assert.True(t, c.mustRetrieve())

	// Creds have not changed
	assert.True(t, c.Creds() == cr)
}

func credsVal(prov string) credentials.Value {
	return credentials.Value{
		AccessKeyID:     "ID",
		SecretAccessKey: "SECRET",
		SessionToken:    "TOKEN",
		ProviderName:    prov,
	}
}

func nilVal(prov string) credentials.Value {
	return credentials.Value{ProviderName: prov}
}
