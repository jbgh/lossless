// Package crypt encrypts backup objects. One master key on disk; HKDF
// derives a content key and a name key; every object gets its own key
// from the content key, a random salt, and its relative path.
package crypt

import (
	"crypto/hkdf"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
)

type Keys struct {
	content []byte
	name    []byte
}

// NewKeyHex returns 32 random bytes as 64 hex characters.
func NewKeyHex() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

// ParseKeyHex accepts the file contents of backup.key.
func ParseKeyHex(s string) (*Keys, error) {
	raw, err := hex.DecodeString(strings.TrimSpace(s))
	if err != nil || len(raw) != 32 {
		return nil, errors.New("backup.key must be 64 hex characters")
	}
	content, err := hkdf.Key(sha256.New, raw, nil, "lossless-backup-content", 32)
	if err != nil {
		return nil, err
	}
	name, err := hkdf.Key(sha256.New, raw, nil, "lossless-backup-name", 32)
	if err != nil {
		return nil, err
	}
	return &Keys{content: content, name: name}, nil
}

// Name is the opaque object-name component for a relative path.
func (k *Keys) Name(relpath string) string {
	m := hmac.New(sha256.New, k.name)
	m.Write([]byte(relpath))
	return hex.EncodeToString(m.Sum(nil))[:32]
}

func (k *Keys) objectKey(salt []byte, relpath string) ([]byte, error) {
	return hkdf.Key(sha256.New, k.content, salt, relpath, 32)
}
