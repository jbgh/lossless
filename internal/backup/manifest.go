package backup

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"lossless/internal/backup/crypt"
	"lossless/internal/backup/s3"
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

var ErrWrongKey = errors.New("backup.key does not match this bucket")

// remote is a bucket plus the key that encrypts everything in it.
type remote struct {
	c    *s3.Client
	keys *crypt.Keys
}

// hintForbidden appends a hint about the likely missing permission to a 403
// on the pointer GET: on AWS this almost always means the credentials lack
// s3:ListBucket on the bucket (an object GET falls back to a bucket-level
// check when the key itself isn't otherwise permitted).
func hintForbidden(err error) error {
	var se *s3.StatusError
	if errors.As(err, &se) && se.Status == 403 {
		return fmt.Errorf("%w (a 403 on the first GET usually means the credentials lack s3:ListBucket on the bucket; see docs/deploy.md)", err)
	}
	return err
}

func (r *remote) getManifest(ctx context.Context, key string) (*Manifest, error) {
	body, _, err := r.c.Get(ctx, key)
	if err != nil {
		if key == pointerKey {
			return nil, hintForbidden(err)
		}
		return nil, err
	}
	defer body.Close()
	var plain bytes.Buffer
	if _, err := r.keys.Decrypt(&plain, body, manifestAAD); err != nil {
		if errors.Is(err, crypt.ErrCorrupt) {
			return nil, ErrWrongKey
		}
		return nil, err
	}
	var m Manifest
	if err := json.Unmarshal(plain.Bytes(), &m); err != nil {
		return nil, fmt.Errorf("manifest %s: %w", key, err)
	}
	if m.Version != manifestVersion {
		return nil, fmt.Errorf("manifest %s: unsupported version %d", key, m.Version)
	}
	if m.Files == nil {
		m.Files = map[string]FileEntry{}
	}
	return &m, nil
}

func (r *remote) putManifest(ctx context.Context, key string, m *Manifest) error {
	plain, err := json.Marshal(m)
	if err != nil {
		return err
	}
	var ct bytes.Buffer
	_, cipherSHA, n, err := r.keys.Encrypt(&ct, bytes.NewReader(plain), manifestAAD)
	if err != nil {
		return err
	}
	return r.c.Put(ctx, key, bytes.NewReader(ct.Bytes()), n, cipherSHA)
}
