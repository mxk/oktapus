package op

import (
	"bytes"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFlags(t *testing.T) {
	f := CredsFlag | LoadFlag | CtlFlag
	assert.True(t, f.CredsValid())
	assert.True(t, f.CtlValid())

	f.Clear(CredsFlag | CtlFlag)
	assert.False(t, f.CredsValid())
	assert.False(t, f.CtlValid())

	f.Set(CredsFlag | CtlFlag)
	f.Clear(LoadFlag)
	assert.True(t, f.CredsValid())
	assert.True(t, f.CtlValid())

	f.Set(LoadFlag | CtlFlag)
	assert.True(t, f.Test(CredsFlag))
	assert.True(t, f.CtlValid())
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
		ac := NewAccount("000000000000", test.name)
		ac.IAM = newCtlIAM().iam
		acs[i] = ac
		require.NoError(t, test.ctl.Init(ac.IAM))
	}

	// Get
	acs.LoadCtl(false)
	for i, ac := range acs {
		require.NoError(t, ac.Err)
		want := tests[i].ctl
		assert.Equal(t, want, ac.Ctl)
		assert.Equal(t, want, ac.ref)
	}

	// Save
	acs[1].Ctl.Owner = ""
	acs[2].Ctl.Owner = "c"
	acs[2].Ctl.Tags[0] = "x"
	acs.StoreCtl()
	for _, ac := range acs {
		require.NoError(t, ac.Err)
		var ctl Ctl
		ctl.Load(ac.IAM)
		assert.Equal(t, ac.Ctl, ctl)
		assert.Equal(t, ac.ref, ctl)
	}
}

func TestAccountCtlErr(t *testing.T) {
	c := newCtlIAM()
	c.err = errors.New("inaccessible")
	ac := NewAccount("000000000000", "error")
	ac.IAM = c.iam

	acs := Accounts{ac}
	acs.StoreCtl()
	assert.Equal(t, ErrNoCtl, ac.Err)

	acs.LoadCtl(false)
	assert.False(t, ac.CtlValid())
	assert.Zero(t, ac.Ctl)
	assert.EqualError(t, ac.Err, "inaccessible")

	ac.CtlUpdate(nil)
	acs.ClearErr().StoreCtl()
	assert.EqualError(t, ac.Err, "inaccessible")

	c.err = ac.CtlUpdate(nil)
	acs.ClearErr().StoreCtl()
	assert.Nil(t, ac.Err)
	assert.Equal(t, ac.Ctl, ac.ref)

	other := &Ctl{Owner: "other"}
	require.NoError(t, other.Store(c.iam))
	ac.Ctl.Owner = "me"
	acs.StoreCtl()
	assert.Equal(t, ErrCtlUpdate, ac.Err)
	assert.NotEqual(t, ac.Ctl, &ac.ref)
	assert.Equal(t, other, &ac.ref)
}

func TestNaturalSort(t *testing.T) {
	tests := []*struct {
		a, b string
		less bool
	}{
		// Equal
		{"", "", false},
		{"a", "A", false},
		{"1", "1", false},
		{"01", "1", false},
		{"a1", "A1", false},
		{"1a2b", "01A02B", false},
		{"a1b2", "A01B02", false},

		// Less
		{"a", "B", true},
		{"2", "10", true},
		{"a2", "A10", true},
		{"a10", "b2", true},
		{"2a", "010A", true},
		{"a-2b", "a-10b", true},
		{"a0b1c02", "a0b01c2", true},
		{"a2b", "a18446744073709551615", true},

		// Invalid uint64
		{"a018446744073709551616", "a18446744073709551616", true},
	}
	for _, tc := range tests {
		a := natSortKey(tc.a)
		b := natSortKey(tc.b)
		if tc.less {
			assert.True(t, a.less(b), "a=%q b=%q", tc.a, tc.b)
			assert.False(t, b.less(a), "a=%q b=%q", tc.a, tc.b)
		} else {
			assert.False(t, a.less(b), "a=%q b=%q", tc.a, tc.b)
			assert.False(t, b.less(a), "a=%q b=%q", tc.a, tc.b)
		}
	}
}
