// Package backup copies raw/, export/, and index snapshots to an
// S3-compatible bucket as encrypted objects with kept generations, and
// restores them. See docs/superpowers/specs/2026-09-12-object-storage-backup-design.md.
package backup

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"lossless/internal/backup/crypt"
	"lossless/internal/backup/s3"
)

var ErrNotConfigured = errors.New("backup not configured (run lossless backup init)")

// ConfigError marks a failure before any network call: missing or malformed
// backup.env, key, URL, or endpoint. The CLI exits 2 on it.
type ConfigError struct{ Err error }

func (e *ConfigError) Error() string { return e.Err.Error() }
func (e *ConfigError) Unwrap() error { return e.Err }

type Config struct {
	URL   string
	S3    s3.Config
	Every time.Duration // 0 = schedule disabled
	Keep  int
}

const defaultKeep = 5

func envPath(home string) string   { return filepath.Join(home, "backup.env") }
func keyPath(home string) string   { return filepath.Join(home, "backup.key") }
func statePath(home string) string { return filepath.Join(home, "backup-state.json") }
func lockPath(home string) string  { return filepath.Join(home, "backup.lock") }
func tmpDir(home string) string    { return filepath.Join(home, "backup-tmp") }

// readEnvFile parses KEY=value lines. Values may be Go-quoted, the same
// shape service.env uses. Blank lines and # comments are skipped. A value
// that starts with a quote but fails to unquote (an unterminated quote, a
// trailing inline comment, ...) is an error naming the key: this file
// holds credentials, so a malformed edit must fail loudly rather than
// silently take the raw, wrong string as the secret.
func readEnvFile(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	out := map[string]string{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		v = strings.TrimSpace(v)
		if strings.HasPrefix(v, `"`) {
			u, err := strconv.Unquote(v)
			if err != nil {
				return nil, fmt.Errorf("%s: %s: malformed quoted value: %w", path, k, err)
			}
			v = u
		}
		out[k] = v
	}
	return out, sc.Err()
}

// LoadConfig reads <home>/backup.env. A process environment variable of
// the same name wins over the file. Credentials fall back to AWS_*.
func LoadConfig(home string) (*Config, error) {
	vals, err := readEnvFile(envPath(home))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrNotConfigured
		}
		return nil, err
	}
	get := func(k string) string {
		if v := os.Getenv(k); v != "" {
			return v
		}
		return vals[k]
	}
	rawURL := get("LOSSLESS_BACKUP_URL")
	if rawURL == "" {
		return nil, ErrNotConfigured
	}
	bucket, prefix, err := s3.ParseURL(rawURL)
	if err != nil {
		return nil, err
	}
	ak := get("LOSSLESS_BACKUP_ACCESS_KEY")
	if ak == "" {
		ak = os.Getenv("AWS_ACCESS_KEY_ID")
	}
	sk := get("LOSSLESS_BACKUP_SECRET_KEY")
	if sk == "" {
		sk = os.Getenv("AWS_SECRET_ACCESS_KEY")
	}
	if ak == "" || sk == "" {
		return nil, fmt.Errorf("backup credentials missing: set LOSSLESS_BACKUP_ACCESS_KEY and LOSSLESS_BACKUP_SECRET_KEY in %s", envPath(home))
	}
	cfg := &Config{
		URL: rawURL,
		S3: s3.Config{
			Bucket: bucket, Prefix: prefix,
			Endpoint: get("LOSSLESS_BACKUP_ENDPOINT"), Region: get("LOSSLESS_BACKUP_REGION"),
			AccessKey: ak, SecretKey: sk, SessionToken: os.Getenv("AWS_SESSION_TOKEN"),
		},
		Keep: defaultKeep,
	}
	if e := strings.TrimSpace(get("LOSSLESS_BACKUP_EVERY")); e != "" && e != "0" {
		d, err := time.ParseDuration(e)
		if err != nil || d < 0 {
			return nil, fmt.Errorf("LOSSLESS_BACKUP_EVERY: %q is not a duration", e)
		}
		cfg.Every = d
	}
	if k := strings.TrimSpace(get("LOSSLESS_BACKUP_KEEP")); k != "" {
		n, err := strconv.Atoi(k)
		if err != nil || n < 1 {
			return nil, fmt.Errorf("LOSSLESS_BACKUP_KEEP: %q must be an integer ≥ 1", k)
		}
		cfg.Keep = n
	}
	return cfg, nil
}

type InitOptions struct {
	URL      string
	Endpoint string
	Region   string
	Every    time.Duration // 0 writes "0" (disabled)
	Keep     int           // 0 means defaultKeep
}

// Init writes backup.env and a fresh backup.key. It refuses to replace an
// existing key. Credentials found in the environment at init time are
// written into backup.env so the daemon can read them later.
func Init(home string, o InitOptions) error {
	if _, _, err := s3.ParseURL(o.URL); err != nil {
		return err
	}
	if o.Endpoint != "" {
		if _, err := s3.New(s3.Config{Bucket: "probe", Endpoint: o.Endpoint}); err != nil {
			return err
		}
	}
	if _, err := os.Stat(keyPath(home)); err == nil {
		return fmt.Errorf("%s exists; refusing to replace a backup key (move it away first if you really mean to)", keyPath(home))
	}
	if err := os.MkdirAll(home, 0o700); err != nil {
		return err
	}
	// Generate the key before writing anything, so a key-generation failure
	// leaves nothing behind.
	keyHex, err := crypt.NewKeyHex()
	if err != nil {
		return err
	}
	keep := o.Keep
	if keep <= 0 {
		keep = defaultKeep
	}
	var b strings.Builder
	fmt.Fprintf(&b, "LOSSLESS_BACKUP_URL=%s\n", strconv.Quote(o.URL))
	if o.Endpoint != "" {
		fmt.Fprintf(&b, "LOSSLESS_BACKUP_ENDPOINT=%s\n", strconv.Quote(o.Endpoint))
	}
	if o.Region != "" {
		fmt.Fprintf(&b, "LOSSLESS_BACKUP_REGION=%s\n", strconv.Quote(o.Region))
	}
	every := "0"
	if o.Every > 0 {
		every = o.Every.String()
	}
	fmt.Fprintf(&b, "LOSSLESS_BACKUP_EVERY=%s\n", strconv.Quote(every))
	fmt.Fprintf(&b, "LOSSLESS_BACKUP_KEEP=%s\n", strconv.Quote(strconv.Itoa(keep)))
	ak := os.Getenv("LOSSLESS_BACKUP_ACCESS_KEY")
	if ak == "" {
		ak = os.Getenv("AWS_ACCESS_KEY_ID")
	}
	sk := os.Getenv("LOSSLESS_BACKUP_SECRET_KEY")
	if sk == "" {
		sk = os.Getenv("AWS_SECRET_ACCESS_KEY")
	}
	if ak != "" && sk != "" {
		fmt.Fprintf(&b, "LOSSLESS_BACKUP_ACCESS_KEY=%s\n", strconv.Quote(ak))
		fmt.Fprintf(&b, "LOSSLESS_BACKUP_SECRET_KEY=%s\n", strconv.Quote(sk))
	} else {
		b.WriteString("# Fill in the bucket credentials (or export AWS_ACCESS_KEY_ID / AWS_SECRET_ACCESS_KEY):\n")
		b.WriteString("# LOSSLESS_BACKUP_ACCESS_KEY=\"\"\n")
		b.WriteString("# LOSSLESS_BACKUP_SECRET_KEY=\"\"\n")
	}
	if err := writeFile0600(envPath(home), []byte(b.String())); err != nil {
		return err
	}
	return writeFile0600(keyPath(home), []byte(keyHex+"\n"))
}

func LoadKey(home string) (*crypt.Keys, error) {
	b, err := os.ReadFile(keyPath(home))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%s missing (copy the key from the machine that ran backup init)", keyPath(home))
		}
		return nil, err
	}
	return crypt.ParseKeyHex(string(b))
}

// writeFile0600 writes tmp-then-rename so a crash never leaves a partial file.
func writeFile0600(path string, data []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	if err := os.Chmod(tmp, 0o600); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, path)
}
