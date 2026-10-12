package env

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// HomeTag identifies a store without publishing its path: serve's /health
// answers with it, so a client can tell whether the daemon it can reach
// serves the home it is about to write (restore refuses to write under a
// daemon holding that home, and under no other). The path is made absolute
// and clean first, so two spellings of one directory agree.
func HomeTag(home string) string {
	if abs, err := filepath.Abs(home); err == nil {
		home = abs
	}
	sum := sha256.Sum256([]byte(filepath.Clean(home)))
	return hex.EncodeToString(sum[:8])
}

func Home() string {
	if h := os.Getenv("LOSSLESS_HOME"); h != "" {
		return h
	}
	return filepath.Join(os.Getenv("HOME"), ".lossless")
}

func URL() string {
	return os.Getenv("LOSSLESS_URL")
}

// CanonicalURL trims space, trailing slashes, and a trailing /mcp
// so ask, append, and MCP never double-path when someone exports the MCP URL.
func CanonicalURL(raw string) string {
	s := strings.TrimSpace(raw)
	s = strings.TrimRight(s, "/")
	s = strings.TrimSuffix(s, "/mcp")
	return strings.TrimRight(s, "/")
}

func BaseURL() string {
	return CanonicalURL(os.Getenv("LOSSLESS_URL"))
}

func Token() string {
	return os.Getenv("LOSSLESS_TOKEN")
}

func NewToken() (string, error) {
	var b [24]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("token: %w", err)
	}
	return hex.EncodeToString(b[:]), nil
}

func Sidecar() string {
	u := strings.TrimSpace(os.Getenv("LOSSLESS_SIDECAR"))
	if u == "off" || u == "0" || u == "-" {
		return ""
	}
	if u != "" {
		return u
	}
	return "http://127.0.0.1:7432"
}

func Client() string {
	return os.Getenv("LOSSLESS_CLIENT")
}
