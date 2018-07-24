package internal

import (
	"bytes"
	"errors"
	"io/ioutil"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTime(t *testing.T) {
	d := time.Now().Sub(Time())
	assert.True(t, d >= 0)
	assert.True(t, d <= 1500*time.Millisecond)

	SetTime(time.Unix(1, 0))
	assert.Equal(t, time.Unix(1, 0), Time())

	SetTime(time.Time{})
	d = time.Now().Sub(Time())
	assert.True(t, d >= 0)
	assert.True(t, d <= 1500*time.Millisecond)
}

func TestSleep(t *testing.T) {
	d := 50 * time.Millisecond
	now := time.Now()
	Sleep(d)
	assert.True(t, time.Since(now) >= d)

	NoSleep(true)
	defer NoSleep(false)

	now = time.Now()
	Sleep(d)
	assert.True(t, time.Since(now) < 25*time.Millisecond)
}

func TestCloseBody(t *testing.T) {
	b := bytes.NewReader(make([]byte, 4097))
	CloseBody(ioutil.NopCloser(b))
	assert.Equal(t, 1, b.Len())
}

func TestJSON(t *testing.T) {
	assert.Equal(t, "{}\n", JSON(struct{}{}))
}

func TestStringsEq(t *testing.T) {
	tests := []*struct {
		a, b []string
		eq   bool
	}{
		{[]string{}, []string{}, true},
		{[]string{"a"}, []string{}, false},
		{[]string{"a"}, []string{"b", "c"}, false},
		{[]string{"a"}, []string{"b"}, false},
		{[]string{"a"}, []string{"a"}, true},
		{[]string{"a", "b"}, []string{"a", "B"}, false},
		{[]string{"a", "b"}, []string{"a", "b"}, true},
	}
	for _, test := range tests {
		assert.Equal(t, test.eq, StringsEq(test.a, test.b),
			"a=%v b=%v", test.a, test.b)
	}
}

func TestGoForEach(t *testing.T) {
	for n := 0; n <= 256; {
		b, c := bytes.Repeat([]byte{'0'}, n), byte('1')
		for batch := range []int{0, 1, 2, n / 2, n, n * 2} {
			GoForEach(n, batch, func(i int) error {
				require.Equal(t, c-1, b[i])
				b[i] = c
				return nil
			})
			require.Equal(t, strings.Repeat(string(c), n), string(b))
			c++
		}
		if n *= 2; n == 0 {
			n = 1
		}
	}

	require.NoError(t, GoForEach(0, 0, func(i int) error {
		return errors.New("fail")
	}))
	require.Error(t, GoForEach(1, 0, func(i int) error {
		return errors.New("pass")
	}))
	require.Error(t, GoForEach(4, 2, func(i int) error {
		if i == 3 {
			return errors.New("pass")
		}
		return nil
	}))

	b := bytes.Repeat([]byte{'0'}, 50)
	require.Error(t, GoForEach(len(b), len(b), func(i int) error {
		if i == 10 {
			return errors.New("")
		}
		b[i] = '1'
		return nil
	}))
	want := bytes.Repeat([]byte{'1'}, 50)
	want[10] = '0'
	require.Equal(t, string(want), string(b))

	b = bytes.Repeat([]byte{'0'}, 50)
	require.Error(t, GoForEach(len(b), 2, func(i int) error {
		if i == 10 {
			return errors.New("")
		}
		b[i] = '1'
		return nil
	}))
	require.Equal(t, strings.Repeat("1", 10), string(b[:10]))
	require.Equal(t, strings.Repeat("0", 30), string(b[20:]))
}
