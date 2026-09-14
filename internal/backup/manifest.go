package backup

import (
	"crypto/rand"
	"encoding/hex"
	"time"
)

const manifestVersion = 1

// Keys in the bucket. All are relative to the configured prefix.
const (
	pointerKey     = "manifest"
	manifestPrefix = "m/"
	objectPrefix   = "o/"
	manifestAAD    = "manifest" // relpath bound into every manifest's encryption
)

type FileEntry struct {
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
	Object string `json:"object"`
}

// Manifest is one generation. The pointer holds a copy of the newest one.
type Manifest struct {
	Version     int                  `json:"version"`
	Generation  string               `json:"generation"`
	CreatedAt   string               `json:"created_at"`
	Lossless    string               `json:"lossless"`
	Client      string               `json:"client"`
	Generations []string             `json:"generations"`
	Dropping    []string             `json:"dropping"`
	Files       map[string]FileEntry `json:"files"`
}

func emptyManifest() *Manifest {
	return &Manifest{Version: manifestVersion, Files: map[string]FileEntry{}}
}

// NewGeneration is a UTC timestamp plus four random hex characters, so two
// installs writing at the same second never collide.
func NewGeneration(now time.Time) string {
	var b [2]byte
	_, _ = rand.Read(b[:])
	return now.UTC().Format("20060102T150405Z") + "-" + hex.EncodeToString(b[:])
}

func manifestKey(generation string) string { return manifestPrefix + generation }

func objectKey(name, sha string) string { return objectPrefix + name + "/" + sha }
