package backup

import (
	"encoding/json"
	"os"
	"time"
)

type FileState struct {
	Size     int64  `json:"size"`                // bytes in the manifest (snapshot size for sqlite)
	StatSize int64  `json:"stat_size,omitempty"` // on-disk size when hashed (main + -wal for sqlite)
	Mtime    int64  `json:"mtime_ns"`
	SHA256   string `json:"sha256"`
	Object   string `json:"object"`
}

// State is the local cache at <home>/backup-state.json. It is never
// authoritative: the remote pointer is. Losing it costs one full re-hash.
type State struct {
	Files          map[string]FileState `json:"files"`
	LastOK         string               `json:"last_ok,omitempty"`
	LastAttempt    string               `json:"last_attempt,omitempty"`
	LastError      string               `json:"last_error,omitempty"`
	LastGeneration string               `json:"last_generation,omitempty"`
	Adopted        []string             `json:"adopted,omitempty"`
	Manifests      map[string]*Manifest `json:"manifests,omitempty"`
}

func LoadState(home string) *State {
	s := &State{Files: map[string]FileState{}, Manifests: map[string]*Manifest{}}
	b, err := os.ReadFile(statePath(home))
	if err != nil {
		return s
	}
	var loaded State
	if json.Unmarshal(b, &loaded) != nil {
		return s
	}
	if loaded.Files == nil {
		loaded.Files = map[string]FileState{}
	}
	if loaded.Manifests == nil {
		loaded.Manifests = map[string]*Manifest{}
	}
	return &loaded
}

func (s *State) Save(home string) error {
	b, err := json.MarshalIndent(s, "", " ")
	if err != nil {
		return err
	}
	return writeFile0600(statePath(home), b)
}

func (s *State) LastOKTime() (time.Time, bool) {
	if s.LastOK == "" {
		return time.Time{}, false
	}
	t, err := time.Parse(time.RFC3339, s.LastOK)
	return t, err == nil
}

// Adopt records an install id this one may succeed as the bucket writer.
func (s *State) Adopt(client string) {
	if client == "" {
		return
	}
	for _, c := range s.Adopted {
		if c == client {
			return
		}
	}
	s.Adopted = append(s.Adopted, client)
}

// AllowsWriter says whether a pointer last written by client may be
// overwritten by me: first run, same install, or an adopted one.
func (s *State) AllowsWriter(me, client string) bool {
	if client == "" || client == me {
		return true
	}
	for _, c := range s.Adopted {
		if c == client {
			return true
		}
	}
	return false
}
