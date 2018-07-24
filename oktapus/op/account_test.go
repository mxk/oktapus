package op

import (
	"bytes"
	"errors"
	"testing"
	"time"

	"github.com/LuminalHQ/cloudcover/oktapus/awsx"
	"github.com/LuminalHQ/cloudcover/oktapus/internal"
	"github.com/LuminalHQ/cloudcover/oktapus/mock"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAccountCreds(t *testing.T) {
	ac := NewAccount("ID", "Name")
	_, err := ac.Creds(false)
	assert.Error(t, err)

	v := credentials.Value{
		AccessKeyID:     "id",
		SecretAccessKey: "secret",
		SessionToken:    "token",
	}
	exp := internal.Time().Add(time.Minute)
	ac.Init(mock.NewSession(), &awsx.StaticCreds{Value: v, Exp: exp})
	assert.NotNil(t, ac.IAM())

	cr, err := ac.Creds(true)
	require.NoError(t, err)
	assert.Equal(t, v, cr.Value)
}

func TestAccountFilter(t *testing.T) {
	acs := Accounts{
		{Name: "a"},
		{Name: "b"},
		{Name: "c"},
	}
	tests := []*struct {
		fn   func(ac *Account) bool
		want string
	}{{
		fn:   func(ac *Account) bool { return false },
		want: "",
	}, {
		fn:   func(ac *Account) bool { return ac.Name == "a" },
		want: "a",
	}, {
		fn:   func(ac *Account) bool { return ac.Name == "b" },
		want: "b",
	}, {
		fn:   func(ac *Account) bool { return ac.Name == "c" },
		want: "c",
	}, {
		fn:   func(ac *Account) bool { return ac.Name < "c" },
		want: "a,b",
	}, {
		fn:   func(ac *Account) bool { return ac.Name > "a" },
		want: "b,c",
	}, {
		fn:   func(ac *Account) bool { return ac.Name != "b" },
		want: "a,c",
	}, {
		fn:   func(ac *Account) bool { return true },
		want: "a,b,c",
	}}
	var match Accounts
	var buf bytes.Buffer
	for _, test := range tests {
		match = append(match[:0], acs...).Filter(test.fn)
		buf.Reset()
		for i, ac := range match {
			if i > 0 {
				buf.WriteByte(',')
			}
			buf.WriteString(ac.Name)
		}
		assert.Equal(t, test.want, buf.String())
	}
}

func TestAccountCtl(t *testing.T) {
	tests := []*struct {
		name string
		ctl  Ctl
	}{{
		name: "a",
		ctl:  Ctl{Owner: "a"},
	}, {
		name: "b",
		ctl:  Ctl{Owner: "a", Desc: "b"},
	}, {
		name: "c",
		ctl:  Ctl{Desc: "c", Tags: Tags{"c"}},
	}}
	acs := make(Accounts, len(tests))
	for i, test := range tests {
		ac := NewAccount(test.name, test.name)
		ac.iam = new(ctlIAM)
		acs[i] = ac
		require.NoError(t, test.ctl.Init(ac.iam))
	}

	// Get
	acs.RequireCtl()
	for i, ac := range acs {
		require.NoError(t, ac.Err)
		want := &tests[i].ctl
		assert.Equal(t, want, ac.Ctl)
		assert.Equal(t, want, &ac.ref)
	}

	// Save
	acs[1].Owner = ""
	acs[2].Owner = "c"
	acs[2].Tags[0] = "x"
	acs.Save()
	for _, ac := range acs {
		require.NoError(t, ac.Err)
		var ctl Ctl
		ctl.Get(ac.iam)
		assert.Equal(t, ac.Ctl, &ctl)
		assert.Equal(t, &ac.ref, &ctl)
	}
}

func TestAccountCtlErr(t *testing.T) {
	c := &ctlIAM{err: errors.New("inaccessible")}
	ac := NewAccount("", "error")
	ac.iam = c

	acs := Accounts{ac}
	acs.Save()
	assert.Equal(t, ac.Err, ErrNoCtl)
	ac.Err = nil

	acs.RequireCtl()
	assert.Nil(t, ac.Ctl)
	assert.EqualError(t, ac.Err, "inaccessible")
	ac.Err = nil
	ac.Ctl = new(Ctl)

	acs.Save()
	assert.EqualError(t, ac.Err, "inaccessible")

	c.err = nil
	acs.Save()
	assert.Nil(t, ac.Err)
	assert.Equal(t, ac.Ctl, &ac.ref)

	other := &Ctl{Owner: "other"}
	require.NoError(t, other.Set(c))
	ac.Owner = "me"
	acs.Save()
	assert.Equal(t, ac.Err, errCtlUpdate)
	assert.NotEqual(t, ac.Ctl, &ac.ref)
	assert.Equal(t, other, &ac.ref)
}
