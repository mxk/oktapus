package internal

import (
	"bufio"
	"encoding/gob"
	"io/ioutil"
	"os"
	"path/filepath"
	"time"
)

// State preserves information across process invocations.
type State struct {
	cache   map[string][]byte
	dirty   map[string]bool
	file    string
	size    int64
	modTime time.Time
}

// NewState creates a new state backed by the specified file. An empty file name
// creates memory-only state.
func NewState(file string) *State {
	return &State{dirty: make(map[string]bool), file: file}
}

// File returns the state file name.
func (s *State) File() string {
	return s.file
}

// Get returns the state associated with the specified key.
func (s *State) Get(key string) []byte {
	if s.cache == nil {
		s.load()
	}
	return s.cache[key]
}

// Set updates the state associated with the specified key. If value is nil, the
// key is deleted.
func (s *State) Set(key string, value []byte) {
	if s.cache == nil {
		s.load()
	}
	if value != nil {
		s.cache[key] = value
	} else {
		delete(s.cache, key)
	}
	s.dirty[key] = true
}

// Dirty returns true if the state has any unsaved changes.
func (s *State) Dirty() bool {
	return len(s.dirty) > 0
}

// Modified returns true if the state file was modified by another process.
func (s *State) Modified() bool {
	if s.file == "" {
		return false
	}
	fi, err := os.Stat(s.file)
	return err == nil && (fi.Size() != s.size || !fi.ModTime().Equal(s.modTime))
}

// Update loads any new data from the state file. Unsaved data is unaffected.
// Call this method just before saving to merge states.
func (s *State) Update() {
	if prev := s.cache; prev != nil && s.Modified() {
		s.load()
		for k := range s.dirty {
			if v, ok := prev[k]; ok {
				s.cache[k] = v
			} else {
				delete(s.cache, k)
			}
		}
	}
}

// Save writes state to the state file.
func (s *State) Save() {
	for k := range s.dirty {
		delete(s.dirty, k)
	}
	s.size, s.modTime = 0, time.Time{}
	if s.file == "" {
		return
	} else if len(s.cache) == 0 {
		if err := os.Remove(s.file); err != nil && !os.IsNotExist(err) {
			Log.W("Failed to remove state file: %v", err)
		}
		return
	}
	f, err := ioutil.TempFile(filepath.Dir(s.file), filepath.Base(s.file)+".")
	if err != nil {
		Log.W("Failed to create state file: %v", err)
		return
	}
	tmp := f.Name()
	defer func() {
		if f != nil {
			f.Close()
			os.Remove(tmp)
		}
	}()
	buf := bufio.NewWriter(f)
	if err = gob.NewEncoder(buf).Encode(s.cache); err == nil {
		if err = buf.Flush(); err == nil {
			err = f.Close()
		}
	}
	if err != nil {
		Log.W("Failed to write state file: %v", err)
		return
	}
	fi, statErr := os.Stat(tmp)
	if err = os.Rename(tmp, s.file); err == nil {
		f = nil
		s.setStat(fi, statErr)
	} else {
		Log.W("Failed to rename state file: %v", err)
	}
}

// load reads state from the state file.
func (s *State) load() {
	s.cache = make(map[string][]byte)
	if s.file == "" {
		return
	}
	s.size, s.modTime = 0, time.Time{}
	if f, err := os.Open(s.file); err == nil {
		defer f.Close()
		if err = gob.NewDecoder(f).Decode(&s.cache); err == nil {
			s.setStat(f.Stat())
		} else {
			Log.W("Failed to decode state file: %v", err)
			s.cache = make(map[string][]byte)
		}
	} else if !os.IsNotExist(err) {
		Log.W("Failed to open state file: %v", err)
	}
}

// setStat updates state file status after a successful load/save.
func (s *State) setStat(fi os.FileInfo, err error) {
	if err == nil {
		s.size, s.modTime = fi.Size(), fi.ModTime()
	} else {
		Log.W("Failed to get state file status: %v", err)
	}
}
