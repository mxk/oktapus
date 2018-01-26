package internal

import (
	"fmt"
	"io/ioutil"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestStateSaveLoad(t *testing.T) {
	tmp, rm := tmpFile()
	defer rm()

	s := NewState(tmp)
	assert.Equal(t, tmp, s.File())
	assert.False(t, s.Modified())

	assert.Nil(t, s.Get("k1"))
	assert.False(t, s.Dirty())
	s.Set("k1", []byte("v1"))
	assert.True(t, s.Dirty())
	assert.False(t, s.Modified())

	s.Save()
	assert.False(t, s.Dirty())
	assert.False(t, s.Modified())
	assert.Equal(t, []byte("v1"), s.Get("k1"))

	s = NewState(tmp)
	assert.False(t, s.Dirty())
	assert.True(t, s.Modified())
	assert.Equal(t, []byte("v1"), s.Get("k1"))
}

func TestStateUpdate(t *testing.T) {
	tmp, rm := tmpFile()
	defer rm()

	a := NewState(tmp)
	a.Set("k1", []byte("v1"))
	b := NewState(tmp)
	b.Set("k2", []byte("v2"))

	a.Update()
	a.Save()
	b.Update()
	b.Save()

	assert.True(t, a.Modified())
	assert.False(t, b.Modified())

	assert.Equal(t, []byte("v1"), a.Get("k1"))
	assert.Nil(t, a.Get("v2"))
	assert.Equal(t, []byte("v1"), b.Get("k1"))
	assert.Equal(t, []byte("v2"), b.Get("k2"))

	a.Update()
	assert.False(t, a.Modified())
	assert.Equal(t, []byte("v2"), a.Get("k2"))

	a.Set("k1", []byte("a"))
	b.Set("k1", []byte("b"))
	b.Save()
	a.Update()

	assert.Equal(t, []byte("a"), a.Get("k1"))
	assert.Equal(t, []byte("v2"), a.Get("k2"))
	assert.Equal(t, []byte("b"), b.Get("k1"))
	assert.Equal(t, []byte("v2"), b.Get("k2"))

	assert.False(t, a.Modified())
	assert.False(t, b.Modified())
	fi1, err1 := os.Stat(tmp)
	a.Save()
	fi2, err2 := os.Stat(tmp)
	assert.True(t, b.Modified())

	assert.NoError(t, err1)
	assert.NoError(t, err2)

	fmt.Printf("size=%v mod=%v\n", fi1.Size(), fi1.ModTime())
	fmt.Printf("size=%v mod=%v\n", fi2.Size(), fi2.ModTime())

	b.Set("k2", nil)
	b.Update()

	assert.Equal(t, []byte("a"), a.Get("k1"))
	assert.Equal(t, []byte("v2"), a.Get("k2"))
	assert.Equal(t, []byte("a"), b.Get("k1"))
	assert.Nil(t, b.Get("k2"))

	b.Save()
	a.Set("k1", nil)
	a.Update()
	a.Save()

	_, err := os.Stat(tmp)
	assert.True(t, os.IsNotExist(err))
}

func tmpFile() (name string, rm func()) {
	f, err := ioutil.TempFile("", AppName+".")
	if err != nil {
		panic(err)
	}
	name = f.Name()
	rm = func() {
		if err := os.Remove(name); err != nil && !os.IsNotExist(err) {
			panic(err)
		}
	}
	f.Close()
	rm()
	return
}
