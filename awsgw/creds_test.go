package awsgw

import (
	"errors"
	"testing"
	"time"

	"github.com/LuminalHQ/oktapus/internal"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/service/sts"
	"github.com/stretchr/testify/assert"
)

func TestStaticCreds(t *testing.T) {
	exp := internal.Time().Add(time.Minute)
	internal.SetTime(exp.Add(-time.Minute))
	defer internal.SetTime(time.Time{})

	v := credsVal("StaticCreds")
	c := &StaticCreds{Value: v, Exp: exp}

	// Valid
	rv, err := c.Retrieve()
	assert.NoError(t, err)
	assert.Equal(t, v, rv)
	assert.False(t, c.IsExpired())
	assert.Equal(t, exp, c.Expires())

	// Save
	saved := c.Save()
	c.ProviderName = ""
	assert.Equal(t, c, saved)

	// Reset
	c.Reset()
	rv, err = c.Retrieve()
	assert.True(t, err == ErrCredsExpired)
	assert.Equal(t, nilVal("StaticCreds"), rv)
	assert.True(t, c.IsExpired())
	assert.Zero(t, c.Expires())
}

func TestStaticCredsExp(t *testing.T) {
	exp := internal.Time().Add(time.Minute)
	internal.SetTime(exp.Add(-time.Minute))
	defer internal.SetTime(time.Time{})

	v := credsVal("StaticCreds")
	c := &StaticCreds{Value: v, Exp: exp}

	// Valid
	rv, err := c.Retrieve()
	assert.NoError(t, err)
	assert.Equal(t, v, rv)

	// Expired
	internal.SetTime(exp)
	rv, err = c.Retrieve()
	assert.True(t, err == ErrCredsExpired)
	assert.Equal(t, nilVal("StaticCreds"), rv)
}

func TestSavedCreds(t *testing.T) {
	exp := internal.Time().Add(time.Minute)
	internal.SetTime(exp.Add(-time.Minute))
	defer internal.SetTime(time.Time{})

	savedErr := errors.New("invalid credentials")
	saved := &StaticCreds{Err: savedErr, Exp: exp}
	nextVal := credsVal("StaticCreds")
	next := &StaticCreds{Value: nextVal, Exp: exp.Add(time.Minute)}

	c := NewSavedCreds(saved, next)

	// Saved
	assert.True(t, c.IsExpired())
	assert.Equal(t, exp, c.Expires())
	rv, err := c.Retrieve()
	assert.True(t, err == savedErr)
	assert.Equal(t, nilVal("StaticCreds"), rv)
	sv := c.Save()
	sv.Err = savedErr
	saved.ProviderName = ""
	assert.Equal(t, saved, sv)

	// Next
	internal.SetTime(exp)
	assert.False(t, c.IsExpired())
	assert.Equal(t, exp.Add(time.Minute), c.Expires())
	rv, err = c.Retrieve()
	assert.NoError(t, err)
	assert.Equal(t, nextVal, rv)
	sv = c.Save()
	next.ProviderName = ""
	assert.Equal(t, next, sv)

	// Expired
	internal.SetTime(exp.Add(time.Minute))
	assert.True(t, c.IsExpired())
	assert.Zero(t, c.Expires())
	rv, err = c.Retrieve()
	assert.True(t, err == ErrCredsExpired)
	assert.Equal(t, nilVal("StaticCreds"), rv)
	assert.Nil(t, c.Save())

	// Expired creds are ignored
	assert.Equal(t, next, NewSavedCreds(saved, next))

	// Reset
	exp = internal.Time().Add(time.Minute)
	c = NewSavedCreds(&StaticCreds{Value: nextVal, Exp: exp},
		&StaticCreds{Value: nextVal, Exp: exp.Add(time.Minute)})
	c.Reset()
	assert.True(t, c.IsExpired())
	assert.Zero(t, c.Expires())
	rv, err = c.Retrieve()
	assert.True(t, err == ErrCredsExpired)
	assert.Equal(t, nilVal("StaticCreds"), rv)
}

func TestSAMLCreds(t *testing.T) {
	internal.SetTime(internal.Time())
	defer internal.SetTime(time.Time{})

	api := func(in *sts.AssumeRoleWithSAMLInput) (*sts.AssumeRoleWithSAMLOutput, error) {
		assert.Equal(t, "principal", *in.PrincipalArn)
		assert.Equal(t, "role", *in.RoleArn)
		assert.Equal(t, "saml", *in.SAMLAssertion)
		return &sts.AssumeRoleWithSAMLOutput{
			Credentials: &sts.Credentials{
				AccessKeyId:     aws.String("ID"),
				Expiration:      aws.Time(internal.Time().Add(5 * time.Minute)),
				SecretAccessKey: aws.String("SECRET"),
				SessionToken:    aws.String("TOKEN"),
			},
		}, nil
	}
	c := NewSAMLCreds(api, "principal", "role", "saml")
	v, err := c.Retrieve()
	assert.NoError(t, err)
	assert.Equal(t, credsVal("SAMLCreds"), v)
	assert.False(t, c.IsExpired())

	internal.SetTime(c.Expires())
	assert.True(t, c.IsExpired())

	v, err = c.Retrieve()
	assert.NoError(t, err)
	assert.Equal(t, credsVal("SAMLCreds"), v)
	assert.False(t, c.IsExpired())
}

func TestAssumeRoleCreds(t *testing.T) {
	internal.SetTime(internal.Time())
	defer internal.SetTime(time.Time{})

	api := func(in *sts.AssumeRoleInput) (*sts.AssumeRoleOutput, error) {
		assert.Equal(t, "role", *in.RoleArn)
		assert.Equal(t, "roleSessionName", *in.RoleSessionName)
		return &sts.AssumeRoleOutput{
			Credentials: &sts.Credentials{
				AccessKeyId:     aws.String("ID"),
				Expiration:      aws.Time(internal.Time().Add(5 * time.Minute)),
				SecretAccessKey: aws.String("SECRET"),
				SessionToken:    aws.String("TOKEN"),
			},
		}, nil
	}
	c := NewAssumeRoleCreds(api, "role", "roleSessionName")
	v, err := c.Retrieve()
	assert.NoError(t, err)
	assert.Equal(t, credsVal("AssumeRoleCreds"), v)
}

func TestAssumeRoleErr(t *testing.T) {
	internal.SetTime(internal.Time())
	defer internal.SetTime(time.Time{})

	nextErr := errors.New("call failed")
	api := func(in *sts.AssumeRoleInput) (*sts.AssumeRoleOutput, error) {
		if nextErr != nil {
			return new(sts.AssumeRoleOutput), nextErr
		}
		return &sts.AssumeRoleOutput{
			Credentials: &sts.Credentials{
				AccessKeyId:     aws.String("ID"),
				Expiration:      aws.Time(internal.Time().Add(5 * time.Minute)),
				SecretAccessKey: aws.String("SECRET"),
				SessionToken:    aws.String("TOKEN"),
			},
		}, nil
	}
	c := NewAssumeRoleCreds(api, "role", "roleSessionName")
	v, err := c.Retrieve()
	assert.True(t, err == nextErr)
	assert.Equal(t, nilVal("AssumeRoleCreds"), v)
	assert.True(t, c.IsExpired())

	// Error is cached
	cachedErr := nextErr
	nextErr = nil
	v, err = c.Retrieve()
	assert.True(t, err == cachedErr)
	assert.Equal(t, nilVal("AssumeRoleCreds"), v)
	assert.True(t, c.IsExpired())

	// Error expired
	internal.SetTime(c.Expires())
	v, err = c.Retrieve()
	assert.NoError(t, err)
	assert.Equal(t, credsVal("AssumeRoleCreds"), v)
	assert.False(t, c.IsExpired())

	// Reset
	c.Reset()
	assert.True(t, c.IsExpired())
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
