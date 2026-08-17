package env

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

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
	if u := os.Getenv("LOSSLESS_SIDECAR"); u != "" {
		return u
	}
	return "http://127.0.0.1:7432"
}

func Client() string {
	return os.Getenv("LOSSLESS_CLIENT")
}
