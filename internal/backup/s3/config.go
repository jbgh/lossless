package s3

import (
	"fmt"
	"net/url"
	"strings"

	"lossless/internal/write"
)

// Config is everything the client needs. Endpoint empty means AWS with
// virtual-host addressing; any endpoint means path-style addressing.
type Config struct {
	Bucket       string
	Prefix       string
	Endpoint     string
	Region       string
	AccessKey    string
	SecretKey    string
	SessionToken string
}

// ParseURL splits s3://bucket/prefix. Prefix may be empty and never keeps
// a leading or trailing slash.
func ParseURL(s string) (bucket, prefix string, err error) {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "s3://") {
		return "", "", fmt.Errorf("backup URL must start with s3://")
	}
	rest := strings.TrimPrefix(s, "s3://")
	bucket, prefix, _ = strings.Cut(rest, "/")
	if bucket == "" {
		return "", "", fmt.Errorf("backup URL has no bucket")
	}
	prefix = strings.Trim(prefix, "/")
	return bucket, prefix, nil
}

func (c Config) normalized() (Config, error) {
	if strings.TrimSpace(c.Bucket) == "" {
		return c, fmt.Errorf("bucket is empty")
	}
	c.Prefix = strings.Trim(c.Prefix, "/")
	c.Endpoint = strings.TrimRight(strings.TrimSpace(c.Endpoint), "/")
	if c.Endpoint != "" {
		if err := write.CheckRemoteURL(c.Endpoint); err != nil {
			return c, fmt.Errorf("endpoint: %w", err)
		}
		u, err := url.Parse(c.Endpoint)
		if err != nil {
			return c, fmt.Errorf("endpoint: %w", err)
		}
		if strings.HasSuffix(strings.ToLower(u.Hostname()), ".r2.cloudflarestorage.com") {
			c.Region = "auto"
		}
	}
	if c.Region == "" {
		c.Region = "us-east-1"
	}
	return c, nil
}

func (c Config) fullKey(key string) string {
	if c.Prefix == "" {
		return key
	}
	return c.Prefix + "/" + key
}

// objectURL builds the request URL. Each path segment is escaped the way
// SigV4 expects (RFC 3986 unreserved set), and "/" is kept as a separator.
func (c Config) objectURL(key string) (*url.URL, error) {
	full := c.fullKey(key)
	if c.Endpoint == "" {
		u := &url.URL{Scheme: "https", Host: c.Bucket + ".s3." + c.Region + ".amazonaws.com"}
		u.Path = "/" + full
		u.RawPath = "/" + escapePath(full)
		return u, nil
	}
	u, err := url.Parse(c.Endpoint)
	if err != nil {
		return nil, err
	}
	u.Path = "/" + c.Bucket + "/" + full
	u.RawPath = "/" + escapePath(c.Bucket) + "/" + escapePath(full)
	return u, nil
}

// escapePath percent-encodes every byte outside A-Za-z0-9-_.~ except "/".
func escapePath(p string) string {
	const hex = "0123456789ABCDEF"
	var b strings.Builder
	for i := 0; i < len(p); i++ {
		ch := p[i]
		switch {
		case ch >= 'A' && ch <= 'Z', ch >= 'a' && ch <= 'z', ch >= '0' && ch <= '9',
			ch == '-', ch == '_', ch == '.', ch == '~', ch == '/':
			b.WriteByte(ch)
		default:
			b.WriteByte('%')
			b.WriteByte(hex[ch>>4])
			b.WriteByte(hex[ch&15])
		}
	}
	return b.String()
}
