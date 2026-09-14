# Object Storage Backup Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Opt-in, encrypted, incremental backup of the lossless store (`raw/`, `export/`, sqlite index snapshots) to an S3-compatible bucket, with kept generations, a writer guard, restore, and a daemon-side schedule.

**Architecture:** Three new packages under `internal/backup/`: `s3` (hand-rolled SigV4 client), `crypt` (per-object HKDF keys, chunked AES-256-GCM), and `backup` (config, state cache, walk, plan, run, restore, scheduler). One new `store.Snapshot` helper runs `VACUUM INTO`. CLI wiring lives in a new `cmd/lossless/backup.go`; the scheduler goroutine starts next to the watcher in `serve.Listen`. Nothing changes when `backup.env` is absent.

**Tech Stack:** Go 1.24+ stdlib only (`net/http`, `crypto/hkdf`, `crypto/aes`, `crypto/cipher`, `crypto/hmac`, `syscall.Flock`), `modernc.org/sqlite` (already a dependency), `httptest` fakes for every network test.

**Spec:** `docs/superpowers/specs/2026-09-12-object-storage-backup-design.md`

## Global Constraints

- No new Go modules. `go.mod` keeps exactly `github.com/klauspost/compress` and `modernc.org/sqlite` as direct requirements.
- Only three roots are ever read for backup: `raw/`, `export/`, `index/`. Nothing else under the home.
- Hooks never wait on the network. A live part is held under shared flocks (the part and its `.lock` sidecar) only while it is copied to `backup-tmp/`.
- Manifest pointer is written last. A run that fails before the pointer PUT leaves the previous generation restorable.
- Region signs as `auto` on any host ending in `.r2.cloudflarestorage.com`.
- Every PUT carries a signed payload hash (`x-amz-content-sha256` = sha256 of the body). Never `UNSIGNED-PAYLOAD`.
- No LIST operation anywhere.
- Store dirs `0700`, files `0600`. `backup.env`, `backup.key`, `backup-state.json` are `0600`.
- CI runs `gofmt -l .`, `go vet ./...`, `go test ./...`. Every task ends green on all three.
- Commit messages are one imperative line, matching the repo history (`Add …`, `Ship …`).

---

## File Structure

| File | Responsibility |
|------|----------------|
| `internal/backup/s3/config.go` | `Config`, `ParseURL` (`s3://bucket/prefix`), endpoint and region rules, key joining |
| `internal/backup/s3/sign.go` | SigV4: canonical request, string to sign, signing key, `Authorization` header |
| `internal/backup/s3/client.go` | `Client` with `Put`, `Get`, `Head`, `Delete`, retry with backoff, `StatusError`, `ErrNotFound` |
| `internal/backup/s3/s3test/server.go` | In-process fake S3 for tests in any package: map store, header checks, failure injection, R2-style 429 |
| `internal/backup/crypt/keys.go` | Master key parse/generate, HKDF sub-keys, HMAC path namer |
| `internal/backup/crypt/stream.go` | Chunked AES-256-GCM `Encrypt` / `Decrypt`, `CipherLen` |
| `internal/store/snapshot.go` | `Snapshot(src, dst)`: `VACUUM INTO` on a read connection |
| `internal/backup/config.go` | `backup.env` load/write, `Init`, `LoadKey`, `ErrNotConfigured` |
| `internal/backup/state.go` | `backup-state.json` cache: file hashes, `last_ok`, `adopted`, kept manifests |
| `internal/backup/lock.go` | `backup.lock` flock, `ErrLocked` |
| `internal/backup/manifest.go` | `Manifest` type, generation ids, encrypted get/put of manifests |
| `internal/backup/walk.go` | Walk the three roots into `Item`s; live-part copy under both locks; sqlite snapshots; hash cache |
| `internal/backup/plan.go` | Diff desired set against the pointer; generation keep/drop |
| `internal/backup/run.go` | `Run`: guard, upload, commit pointer, drop generations, summary |
| `internal/backup/restore.go` | `Restore`, `List` |
| `internal/backup/schedule.go` | `Scheduler`: due time from `last_ok`, retry on failure |
| `internal/backup/doctor.go` | One doctor check line |
| `cmd/lossless/backup.go` | `runBackup` (`init` \| run), `runRestore` (run \| `--list`) |
| `cmd/lossless/main.go` | switch entries, usage lines, doctor line |
| `internal/serve/serve.go` | start the scheduler goroutine when `Watch` is on |
| `docs/deploy.md`, `docs/roadmap.md`, `README.md`, `CHANGELOG.md` | docs |

---

### Task 1: S3 config, URL parsing, and the SigV4 signer

**Files:**
- Create: `internal/backup/s3/config.go`
- Create: `internal/backup/s3/sign.go`
- Test: `internal/backup/s3/config_test.go`
- Test: `internal/backup/s3/sign_test.go`

**Interfaces:**
- Consumes: `write.CheckRemoteURL(raw string) error` from `internal/write/push.go` (https unless loopback).
- Produces:
  - `type Config struct { Bucket, Prefix, Endpoint, Region, AccessKey, SecretKey, SessionToken string }`
  - `func ParseURL(s string) (bucket, prefix string, err error)`
  - `func (c Config) normalized() (Config, error)` — fills region, applies the R2 rule, validates endpoint
  - `func (c Config) objectURL(key string) (*url.URL, error)` — path-style with endpoint, virtual-host without
  - `func (c Config) fullKey(key string) string` — prefix join
  - `func sign(req *http.Request, cfg Config, payloadHash string, now time.Time)` — sets `x-amz-date`, `x-amz-content-sha256`, optional `x-amz-security-token`, `Authorization`

- [ ] **Step 1: Write the failing config tests**

```go
// internal/backup/s3/config_test.go
package s3

import "testing"

func TestParseURL(t *testing.T) {
	cases := []struct{ in, bucket, prefix string; ok bool }{
		{"s3://my-bucket/lossless", "my-bucket", "lossless", true},
		{"s3://my-bucket", "my-bucket", "", true},
		{"s3://my-bucket/", "my-bucket", "", true},
		{"s3://my-bucket/a/b/", "my-bucket", "a/b", true},
		{"https://my-bucket/x", "", "", false},
		{"s3:///x", "", "", false},
		{"", "", "", false},
	}
	for _, c := range cases {
		b, p, err := ParseURL(c.in)
		if (err == nil) != c.ok || b != c.bucket || p != c.prefix {
			t.Fatalf("%q: bucket=%q prefix=%q err=%v", c.in, b, p, err)
		}
	}
}

func TestNormalizedRegionAndEndpoint(t *testing.T) {
	c, err := (Config{Bucket: "b"}).normalized()
	if err != nil || c.Region != "us-east-1" {
		t.Fatalf("default region: %+v err=%v", c, err)
	}
	c, err = (Config{Bucket: "b", Endpoint: "https://abc123.r2.cloudflarestorage.com", Region: "us-east-1"}).normalized()
	if err != nil || c.Region != "auto" {
		t.Fatalf("r2 must sign auto: %+v err=%v", c, err)
	}
	c, err = (Config{Bucket: "b", Endpoint: "https://abc123.eu.r2.cloudflarestorage.com"}).normalized()
	if err != nil || c.Region != "auto" {
		t.Fatalf("r2 jurisdiction must sign auto: %+v err=%v", c, err)
	}
	if _, err := (Config{Bucket: "b", Endpoint: "http://minio.example.com:9000"}).normalized(); err == nil {
		t.Fatal("http off loopback must be refused")
	}
	if _, err := (Config{Bucket: "b", Endpoint: "http://127.0.0.1:9000"}).normalized(); err != nil {
		t.Fatalf("loopback http allowed: %v", err)
	}
	if _, err := (Config{}).normalized(); err == nil {
		t.Fatal("empty bucket must be refused")
	}
}

func TestObjectURLAndFullKey(t *testing.T) {
	c := Config{Bucket: "b", Prefix: "p/q"}
	if got := c.fullKey("o/x"); got != "p/q/o/x" {
		t.Fatal(got)
	}
	if got := (Config{Bucket: "b"}).fullKey("manifest"); got != "manifest" {
		t.Fatal(got)
	}
	u, err := (Config{Bucket: "b", Region: "us-east-1"}).objectURL("o/x")
	if err != nil || u.String() != "https://b.s3.us-east-1.amazonaws.com/o/x" {
		t.Fatalf("virtual-host: %v %v", u, err)
	}
	u, err = (Config{Bucket: "b", Prefix: "p", Endpoint: "https://acct.r2.cloudflarestorage.com"}).objectURL("m/g 1")
	if err != nil || u.String() != "https://acct.r2.cloudflarestorage.com/b/p/m/g%201" {
		t.Fatalf("path-style: %v %v", u, err)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/backup/s3/ -run 'TestParseURL|TestNormalized|TestObjectURL' -v`
Expected: FAIL — package does not exist / undefined: ParseURL

- [ ] **Step 3: Write config.go**

```go
// internal/backup/s3/config.go
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
```

- [ ] **Step 4: Run the config tests**

Run: `go test ./internal/backup/s3/ -run 'TestParseURL|TestNormalized|TestObjectURL' -v`
Expected: PASS

- [ ] **Step 5: Write the failing signer test**

The vector is the S3 "GET object" example from the AWS SigV4 documentation
(`docs.aws.amazon.com/AmazonS3/latest/API/sig-v4-header-based-auth.html`).
It was recomputed locally on 2026-09-14 and matches.

```go
// internal/backup/s3/sign_test.go
package s3

import (
	"net/http"
	"testing"
	"time"
)

func TestSignMatchesAWSExample(t *testing.T) {
	cfg := Config{
		Bucket:    "examplebucket",
		Region:    "us-east-1",
		AccessKey: "AKIAIOSFODNN7EXAMPLE",
		SecretKey: "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
	}
	req, _ := http.NewRequest(http.MethodGet, "https://examplebucket.s3.amazonaws.com/test.txt", nil)
	req.Header.Set("Range", "bytes=0-9")
	now := time.Date(2013, 5, 24, 0, 0, 0, 0, time.UTC)
	sign(req, cfg, emptyPayloadHash, now)
	want := "AWS4-HMAC-SHA256 Credential=AKIAIOSFODNN7EXAMPLE/20130524/us-east-1/s3/aws4_request," +
		"SignedHeaders=host;range;x-amz-content-sha256;x-amz-date," +
		"Signature=f0e8bdb87c964420e857bd35b5d6ed310bd44f0170aba48dd91039c6036bdb41"
	if got := req.Header.Get("Authorization"); got != want {
		t.Fatalf("\n got %s\nwant %s", got, want)
	}
	if req.Header.Get("x-amz-date") != "20130524T000000Z" {
		t.Fatal(req.Header.Get("x-amz-date"))
	}
	if req.Header.Get("x-amz-content-sha256") != emptyPayloadHash {
		t.Fatal("payload hash header")
	}
}

func TestSignAddsSessionToken(t *testing.T) {
	cfg := Config{Bucket: "b", Region: "auto", AccessKey: "k", SecretKey: "s", SessionToken: "tok"}
	req, _ := http.NewRequest(http.MethodPut, "https://acct.r2.cloudflarestorage.com/b/x", nil)
	sign(req, cfg, emptyPayloadHash, time.Unix(0, 0).UTC())
	if req.Header.Get("x-amz-security-token") != "tok" {
		t.Fatal("token header missing")
	}
	auth := req.Header.Get("Authorization")
	if !contains(auth, "/auto/s3/aws4_request") || !contains(auth, "x-amz-security-token") {
		t.Fatal(auth)
	}
}

func contains(s, sub string) bool { return len(s) >= len(sub) && (func() bool { for i := 0; i+len(sub) <= len(s); i++ { if s[i:i+len(sub)] == sub { return true } }; return false })() }
```

- [ ] **Step 6: Run the signer tests to verify they fail**

Run: `go test ./internal/backup/s3/ -run TestSign -v`
Expected: FAIL — undefined: sign, emptyPayloadHash

- [ ] **Step 7: Write sign.go**

```go
// internal/backup/s3/sign.go
package s3

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"sort"
	"strings"
	"time"
)

// emptyPayloadHash is sha256("") — the payload hash for GET, HEAD, DELETE.
const emptyPayloadHash = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"

const amzDateLayout = "20060102T150405Z"

// sign adds SigV4 headers to req. payloadHash is the hex sha256 of the
// body (emptyPayloadHash when there is none). Host, x-amz-content-sha256,
// x-amz-date, and any other header already on req are signed.
func sign(req *http.Request, cfg Config, payloadHash string, now time.Time) {
	now = now.UTC()
	amzDate := now.Format(amzDateLayout)
	day := now.Format("20060102")
	req.Header.Set("x-amz-date", amzDate)
	req.Header.Set("x-amz-content-sha256", payloadHash)
	if cfg.SessionToken != "" {
		req.Header.Set("x-amz-security-token", cfg.SessionToken)
	}
	host := req.Host
	if host == "" {
		host = req.URL.Host
	}

	// Canonical headers: lowercase names, trimmed values, sorted by name.
	names := []string{"host"}
	values := map[string]string{"host": host}
	for k, v := range req.Header {
		lk := strings.ToLower(k)
		if lk == "authorization" || lk == "content-length" || lk == "user-agent" {
			continue
		}
		names = append(names, lk)
		values[lk] = strings.TrimSpace(strings.Join(v, ","))
	}
	sort.Strings(names)
	var canonHeaders strings.Builder
	for _, n := range names {
		canonHeaders.WriteString(n)
		canonHeaders.WriteByte(':')
		canonHeaders.WriteString(values[n])
		canonHeaders.WriteByte('\n')
	}
	signedHeaders := strings.Join(names, ";")

	path := req.URL.EscapedPath()
	if path == "" {
		path = "/"
	}
	canonical := strings.Join([]string{
		req.Method,
		path,
		canonicalQuery(req.URL.RawQuery),
		canonHeaders.String(),
		signedHeaders,
		payloadHash,
	}, "\n")

	scope := day + "/" + cfg.Region + "/s3/aws4_request"
	stringToSign := strings.Join([]string{
		"AWS4-HMAC-SHA256",
		amzDate,
		scope,
		sha256Hex([]byte(canonical)),
	}, "\n")

	kDate := hmacSHA256([]byte("AWS4"+cfg.SecretKey), day)
	kRegion := hmacSHA256(kDate, cfg.Region)
	kService := hmacSHA256(kRegion, "s3")
	kSigning := hmacSHA256(kService, "aws4_request")
	signature := hex.EncodeToString(hmacSHA256(kSigning, stringToSign))

	req.Header.Set("Authorization", "AWS4-HMAC-SHA256 Credential="+cfg.AccessKey+"/"+scope+
		",SignedHeaders="+signedHeaders+",Signature="+signature)
}

// canonicalQuery sorts key=value pairs. Our requests never carry a query,
// but the signer stays correct if one appears.
func canonicalQuery(raw string) string {
	if raw == "" {
		return ""
	}
	parts := strings.Split(raw, "&")
	sort.Strings(parts)
	return strings.Join(parts, "&")
}

func hmacSHA256(key []byte, msg string) []byte {
	m := hmac.New(sha256.New, key)
	m.Write([]byte(msg))
	return m.Sum(nil)
}

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
```

- [ ] **Step 8: Run all s3 tests**

Run: `go test ./internal/backup/s3/ -v`
Expected: PASS (5 tests)

- [ ] **Step 9: gofmt, vet, commit**

```bash
gofmt -l internal/backup && go vet ./internal/backup/... && \
git add internal/backup/s3 && \
git commit -m "Add backup s3 config and SigV4 signer."
```

---

### Task 2: S3 client operations, retry, and the fake server

**Files:**
- Create: `internal/backup/s3/client.go`
- Create: `internal/backup/s3/s3test/server.go`
- Test: `internal/backup/s3/client_test.go` (external package `s3_test`, because `s3test` imports `s3`)

**Interfaces:**
- Consumes: `Config`, `sign`, `objectURL`, `emptyPayloadHash` from Task 1.
- Produces:
  - `func New(cfg Config) (*Client, error)`
  - `func (c *Client) Put(ctx context.Context, key string, body io.ReadSeeker, size int64, sha256hex string) error`
  - `func (c *Client) Get(ctx context.Context, key string) (io.ReadCloser, int64, error)` — `ErrNotFound` on 404
  - `func (c *Client) Head(ctx context.Context, key string) (int64, error)` — `ErrNotFound` on 404
  - `func (c *Client) Delete(ctx context.Context, key string) error` — 404 is success
  - `var ErrNotFound = errors.New("s3: not found")`
  - `type StatusError struct { Op, Key string; Status int; Code string }` with `Error()`
  - `s3test.New() *Server`, `(*Server).Close()`, `(*Server).Config(bucket, prefix string) s3.Config`, `(*Server).Object(bucket, key string) ([]byte, bool)`, `(*Server).Keys(bucket string) []string`, `(*Server).FailNext(bucket, key string, status, times int)`, `(*Server).Count(method string) int`, `Server.RateLimit bool`

- [ ] **Step 1: Write the fake server (it is test infrastructure, so it comes first)**

```go
// internal/backup/s3/s3test/server.go
// Package s3test is an in-process S3 double for tests in any package. It
// stores objects in a map keyed "bucket/key", checks the SigV4 header
// shape and the signed payload hash, injects failures per key, and can
// enforce R2's one-write-per-second rule per key.
package s3test

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"time"

	"lossless/internal/backup/s3"
)

type failure struct {
	status string
	times  int
}

type Server struct {
	srv       *httptest.Server
	mu        sync.Mutex
	objects   map[string][]byte
	fails     map[string]*failure
	lastPut   map[string]time.Time
	counts    map[string]int
	RateLimit bool
}

func New() *Server {
	s := &Server{
		objects: map[string][]byte{},
		fails:   map[string]*failure{},
		lastPut: map[string]time.Time{},
		counts:  map[string]int{},
	}
	s.srv = httptest.NewServer(http.HandlerFunc(s.handle))
	return s
}

func (s *Server) Close() { s.srv.Close() }

func (s *Server) URL() string { return s.srv.URL }

// Config points a client at this server with path-style addressing.
func (s *Server) Config(bucket, prefix string) s3.Config {
	return s3.Config{
		Bucket:    bucket,
		Prefix:    prefix,
		Endpoint:  s.srv.URL,
		Region:    "us-east-1",
		AccessKey: "test",
		SecretKey: "secret",
	}
}

func (s *Server) Object(bucket, key string) ([]byte, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, ok := s.objects[bucket+"/"+key]
	return b, ok
}

// Keys lists every key in bucket, sorted. Tests only; the client never lists.
func (s *Server) Keys(bucket string) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []string
	for k := range s.objects {
		if strings.HasPrefix(k, bucket+"/") {
			out = append(out, strings.TrimPrefix(k, bucket+"/"))
		}
	}
	sort.Strings(out)
	return out
}

// FailNext makes the next `times` requests for bucket/key answer status
// (an HTTP code as a string like "500", or "drop" to close the connection).
func (s *Server) FailNext(bucket, key string, status string, times int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.fails[bucket+"/"+key] = &failure{status: status, times: times}
}

func (s *Server) Count(method string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.counts[method]
}

func (s *Server) handle(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/")
	s.mu.Lock()
	s.counts[r.Method]++
	if f := s.fails[path]; f != nil && f.times > 0 {
		f.times--
		s.mu.Unlock()
		if f.status == "drop" {
			if h, ok := w.(http.Hijacker); ok {
				if c, _, err := h.Hijack(); err == nil {
					_ = c.Close()
				}
			}
			return
		}
		var code int
		fmt.Sscanf(f.status, "%d", &code)
		xmlError(w, code, "InjectedFailure")
		return
	}
	s.mu.Unlock()

	auth := r.Header.Get("Authorization")
	if !strings.HasPrefix(auth, "AWS4-HMAC-SHA256 Credential=test/") ||
		!strings.Contains(auth, "SignedHeaders=host;x-amz-content-sha256;x-amz-date") ||
		!strings.Contains(auth, ",Signature=") {
		xmlError(w, 403, "SignatureDoesNotMatch")
		return
	}
	if r.Header.Get("x-amz-content-sha256") == "" || r.Header.Get("x-amz-date") == "" {
		xmlError(w, 400, "InvalidRequest")
		return
	}

	switch r.Method {
	case http.MethodPut:
		body, err := io.ReadAll(r.Body)
		if err != nil {
			xmlError(w, 400, "IncompleteBody")
			return
		}
		sum := sha256.Sum256(body)
		if hex.EncodeToString(sum[:]) != r.Header.Get("x-amz-content-sha256") {
			xmlError(w, 400, "XAmzContentSHA256Mismatch")
			return
		}
		s.mu.Lock()
		if s.RateLimit && time.Since(s.lastPut[path]) < time.Second {
			s.mu.Unlock()
			xmlError(w, 429, "SlowDown")
			return
		}
		s.lastPut[path] = time.Now()
		s.objects[path] = body
		s.mu.Unlock()
		w.Header().Set("ETag", `"`+hex.EncodeToString(sum[:16])+`"`)
		w.WriteHeader(200)
	case http.MethodGet, http.MethodHead:
		s.mu.Lock()
		body, ok := s.objects[path]
		s.mu.Unlock()
		if !ok {
			xmlError(w, 404, "NoSuchKey")
			return
		}
		w.Header().Set("Content-Length", fmt.Sprint(len(body)))
		w.WriteHeader(200)
		if r.Method == http.MethodGet {
			_, _ = w.Write(body)
		}
	case http.MethodDelete:
		s.mu.Lock()
		delete(s.objects, path)
		s.mu.Unlock()
		w.WriteHeader(204)
	default:
		xmlError(w, 405, "MethodNotAllowed")
	}
}

func xmlError(w http.ResponseWriter, status int, code string) {
	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(status)
	fmt.Fprintf(w, `<?xml version="1.0" encoding="UTF-8"?><Error><Code>%s</Code><Message>%s</Message></Error>`, code, code)
}
```

- [ ] **Step 2: Write the failing client tests**

```go
// internal/backup/s3/client_test.go
package s3_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"testing"
	"time"

	"lossless/internal/backup/s3"
	"lossless/internal/backup/s3/s3test"
)

func sha(b []byte) string { s := sha256.Sum256(b); return hex.EncodeToString(s[:]) }

func newClient(t *testing.T, srv *s3test.Server) *s3.Client {
	t.Helper()
	c, err := s3.New(srv.Config("bkt", "pre"))
	if err != nil {
		t.Fatal(err)
	}
	c.SetSleep(func(time.Duration) {}) // no real backoff in tests
	return c
}

func TestPutGetHeadDelete(t *testing.T) {
	srv := s3test.New()
	defer srv.Close()
	c := newClient(t, srv)
	ctx := context.Background()
	body := []byte("hello object")
	if err := c.Put(ctx, "o/a/b", bytes.NewReader(body), int64(len(body)), sha(body)); err != nil {
		t.Fatal(err)
	}
	if got, ok := srv.Object("bkt", "pre/o/a/b"); !ok || !bytes.Equal(got, body) {
		t.Fatalf("stored under prefix: %v %q", ok, got)
	}
	n, err := c.Head(ctx, "o/a/b")
	if err != nil || n != int64(len(body)) {
		t.Fatalf("head: %d %v", n, err)
	}
	rc, n, err := c.Get(ctx, "o/a/b")
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(rc)
	rc.Close()
	if !bytes.Equal(got, body) || n != int64(len(body)) {
		t.Fatalf("get: %q %d", got, n)
	}
	if err := c.Delete(ctx, "o/a/b"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Head(ctx, "o/a/b"); !errors.Is(err, s3.ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
	if _, _, err := c.Get(ctx, "o/a/b"); !errors.Is(err, s3.ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
	if err := c.Delete(ctx, "o/a/b"); err != nil {
		t.Fatalf("delete of a missing key is success: %v", err)
	}
}

func TestPutWrongHashIsRejectedWithoutRetry(t *testing.T) {
	srv := s3test.New()
	defer srv.Close()
	c := newClient(t, srv)
	body := []byte("x")
	err := c.Put(context.Background(), "k", bytes.NewReader(body), 1, sha([]byte("y")))
	var se *s3.StatusError
	if !errors.As(err, &se) || se.Status != 400 || se.Code != "XAmzContentSHA256Mismatch" {
		t.Fatalf("want 400 XAmzContentSHA256Mismatch, got %v", err)
	}
	if srv.Count("PUT") != 1 {
		t.Fatalf("400 must not retry: %d puts", srv.Count("PUT"))
	}
}

func TestRetriesOn5xxAnd429ThenSucceeds(t *testing.T) {
	srv := s3test.New()
	defer srv.Close()
	c := newClient(t, srv)
	body := []byte("retry me")
	srv.FailNext("bkt", "pre/k", "503", 2)
	if err := c.Put(context.Background(), "k", bytes.NewReader(body), int64(len(body)), sha(body)); err != nil {
		t.Fatal(err)
	}
	if srv.Count("PUT") != 3 {
		t.Fatalf("want 3 attempts, got %d", srv.Count("PUT"))
	}
	srv.FailNext("bkt", "pre/k2", "429", 1)
	if err := c.Put(context.Background(), "k2", bytes.NewReader(body), int64(len(body)), sha(body)); err != nil {
		t.Fatal(err)
	}
}

func TestGivesUpAfterThreeRetries(t *testing.T) {
	srv := s3test.New()
	defer srv.Close()
	c := newClient(t, srv)
	srv.FailNext("bkt", "pre/k", "500", 10)
	err := c.Put(context.Background(), "k", bytes.NewReader([]byte("a")), 1, sha([]byte("a")))
	var se *s3.StatusError
	if !errors.As(err, &se) || se.Status != 500 {
		t.Fatalf("want 500 after retries, got %v", err)
	}
	if srv.Count("PUT") != 4 {
		t.Fatalf("want 1 + 3 retries, got %d", srv.Count("PUT"))
	}
}

func TestRetriesOnDroppedConnection(t *testing.T) {
	srv := s3test.New()
	defer srv.Close()
	c := newClient(t, srv)
	srv.FailNext("bkt", "pre/k", "drop", 1)
	body := []byte("again")
	if err := c.Put(context.Background(), "k", bytes.NewReader(body), int64(len(body)), sha(body)); err != nil {
		t.Fatal(err)
	}
}

func TestNewRejectsHTTPEndpointOffLoopback(t *testing.T) {
	if _, err := s3.New(s3.Config{Bucket: "b", Endpoint: "http://minio.example.com"}); err == nil {
		t.Fatal("must refuse")
	}
}
```

- [ ] **Step 3: Run the client tests to verify they fail**

Run: `go test ./internal/backup/s3/... -run 'TestPut|TestRetr|TestGives|TestNew' -v`
Expected: FAIL — undefined: s3.New, s3.Client

- [ ] **Step 4: Write client.go**

```go
// internal/backup/s3/client.go
package s3

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"
)

var ErrNotFound = errors.New("s3: not found")

// StatusError is a non-2xx answer the client did not retry (or gave up on).
type StatusError struct {
	Op     string
	Key    string
	Status int
	Code   string
}

func (e *StatusError) Error() string {
	if e.Code != "" {
		return fmt.Sprintf("s3 %s %s: %d %s", e.Op, e.Key, e.Status, e.Code)
	}
	return fmt.Sprintf("s3 %s %s: %d", e.Op, e.Key, e.Status)
}

type Client struct {
	cfg   Config
	http  *http.Client
	now   func() time.Time
	sleep func(time.Duration)
}

var backoff = []time.Duration{time.Second, 4 * time.Second, 16 * time.Second}

func New(cfg Config) (*Client, error) {
	n, err := cfg.normalized()
	if err != nil {
		return nil, err
	}
	return &Client{
		cfg: n,
		http: &http.Client{
			Timeout: 10 * time.Minute,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return fmt.Errorf("redirects disabled")
			},
		},
		now:   time.Now,
		sleep: time.Sleep,
	}, nil
}

// SetSleep replaces the backoff sleeper. Tests pass a no-op.
func (c *Client) SetSleep(f func(time.Duration)) { c.sleep = f }

// Put uploads body (size bytes, hex sha256hex) under key. body must seek so
// a retry can rewind it.
func (c *Client) Put(ctx context.Context, key string, body io.ReadSeeker, size int64, sha256hex string) error {
	res, err := c.do(ctx, "PUT", key, func() (*http.Request, error) {
		if _, err := body.Seek(0, io.SeekStart); err != nil {
			return nil, err
		}
		u, err := c.cfg.objectURL(key)
		if err != nil {
			return nil, err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPut, u.String(), io.NopCloser(body))
		if err != nil {
			return nil, err
		}
		req.ContentLength = size
		sign(req, c.cfg, sha256hex, c.now())
		return req, nil
	})
	if err != nil {
		return err
	}
	drain(res)
	return nil
}

// Get returns the body and its length. The caller closes the body.
func (c *Client) Get(ctx context.Context, key string) (io.ReadCloser, int64, error) {
	res, err := c.do(ctx, "GET", key, func() (*http.Request, error) {
		return c.bodyless(ctx, http.MethodGet, key)
	})
	if err != nil {
		return nil, 0, err
	}
	return res.Body, res.ContentLength, nil
}

func (c *Client) Head(ctx context.Context, key string) (int64, error) {
	res, err := c.do(ctx, "HEAD", key, func() (*http.Request, error) {
		return c.bodyless(ctx, http.MethodHead, key)
	})
	if err != nil {
		return 0, err
	}
	drain(res)
	if cl := res.Header.Get("Content-Length"); cl != "" {
		n, _ := strconv.ParseInt(cl, 10, 64)
		return n, nil
	}
	return res.ContentLength, nil
}

// Delete removes key. A missing key is success.
func (c *Client) Delete(ctx context.Context, key string) error {
	res, err := c.do(ctx, "DELETE", key, func() (*http.Request, error) {
		return c.bodyless(ctx, http.MethodDelete, key)
	})
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	drain(res)
	return nil
}

func (c *Client) bodyless(ctx context.Context, method, key string) (*http.Request, error) {
	u, err := c.cfg.objectURL(key)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, method, u.String(), nil)
	if err != nil {
		return nil, err
	}
	sign(req, c.cfg, emptyPayloadHash, c.now())
	return req, nil
}

// do runs build() and sends the request, retrying on transport errors,
// 429, and 5xx with the fixed backoff. Any other non-2xx returns at once.
// A 404 returns ErrNotFound. A 2xx response is returned with its body open.
func (c *Client) do(ctx context.Context, op, key string, build func() (*http.Request, error)) (*http.Response, error) {
	var last error
	for attempt := 0; attempt <= len(backoff); attempt++ {
		if attempt > 0 {
			c.sleep(backoff[attempt-1])
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
		}
		req, err := build()
		if err != nil {
			return nil, err
		}
		res, err := c.http.Do(req)
		if err != nil {
			last = fmt.Errorf("s3 %s %s: %w", op, key, err)
			continue
		}
		if res.StatusCode >= 200 && res.StatusCode < 300 {
			return res, nil
		}
		code := readErrorCode(res)
		se := &StatusError{Op: op, Key: key, Status: res.StatusCode, Code: code}
		if res.StatusCode == 404 {
			return nil, fmt.Errorf("%w (%s)", ErrNotFound, se.Error())
		}
		if res.StatusCode == 429 || res.StatusCode >= 500 {
			last = se
			continue
		}
		return nil, se
	}
	return nil, last
}

func readErrorCode(res *http.Response) string {
	defer res.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(res.Body, 64<<10))
	var e struct {
		Code string `xml:"Code"`
	}
	if xml.Unmarshal(b, &e) == nil {
		return e.Code
	}
	return ""
}

func drain(res *http.Response) {
	_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 1<<20))
	_ = res.Body.Close()
}
```

- [ ] **Step 5: Run all s3 tests**

Run: `go test ./internal/backup/s3/... -v`
Expected: PASS (11 tests)

- [ ] **Step 6: gofmt, vet, commit**

```bash
gofmt -l internal/backup && go vet ./internal/backup/... && \
git add internal/backup/s3 && \
git commit -m "Add backup s3 client with retry and an in-process fake."
```

---

### Task 3: crypt — keys, path namer, chunked AES-GCM stream

**Files:**
- Create: `internal/backup/crypt/keys.go`
- Create: `internal/backup/crypt/stream.go`
- Test: `internal/backup/crypt/crypt_test.go`

**Interfaces:**
- Consumes: nothing from earlier tasks.
- Produces:
  - `func NewKeyHex() (string, error)` — 32 random bytes as 64 hex chars
  - `func ParseKeyHex(s string) (*Keys, error)`
  - `func (k *Keys) Name(relpath string) string` — 32 hex chars, HMAC of the path
  - `func (k *Keys) Encrypt(dst io.Writer, src io.Reader, relpath string) (plainSHA, cipherSHA string, cipherLen int64, err error)`
  - `func (k *Keys) Decrypt(dst io.Writer, src io.Reader, relpath string) (plainSHA string, err error)`
  - `func CipherLen(plainLen int64) int64`
  - `const ChunkSize = 1 << 20`
  - `var ErrCorrupt`

- [ ] **Step 1: Write the failing tests**

```go
// internal/backup/crypt/crypt_test.go
package crypt

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"testing"
)

func testKeys(t *testing.T) *Keys {
	t.Helper()
	h, err := NewKeyHex()
	if err != nil || len(h) != 64 {
		t.Fatalf("NewKeyHex: %q %v", h, err)
	}
	k, err := ParseKeyHex(h + "\n")
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func sha(b []byte) string { s := sha256.Sum256(b); return hex.EncodeToString(s[:]) }

func TestParseKeyHexRejectsBadInput(t *testing.T) {
	for _, bad := range []string{"", "abc", "zz" + string(make([]byte, 62))} {
		if _, err := ParseKeyHex(bad); err == nil {
			t.Fatalf("accepted %q", bad)
		}
	}
}

func TestNameIsStableAndPathBound(t *testing.T) {
	k := testKeys(t)
	a, b := k.Name("raw/x/y.jsonl.zst"), k.Name("raw/x/y.jsonl.zst")
	if a != b || len(a) != 32 {
		t.Fatalf("%s %s", a, b)
	}
	if k.Name("raw/x/z.jsonl.zst") == a {
		t.Fatal("different paths must not collide")
	}
	if _, err := hex.DecodeString(a); err != nil {
		t.Fatal(err)
	}
}

func TestRoundTripSizes(t *testing.T) {
	k := testKeys(t)
	for _, n := range []int{0, 1, ChunkSize - 1, ChunkSize, ChunkSize + 1, 3*ChunkSize + 7} {
		plain := make([]byte, n)
		_, _ = rand.Read(plain)
		var ct bytes.Buffer
		pSHA, cSHA, cLen, err := k.Encrypt(&ct, bytes.NewReader(plain), "p/a")
		if err != nil {
			t.Fatalf("n=%d: %v", n, err)
		}
		if pSHA != sha(plain) || cSHA != sha(ct.Bytes()) || cLen != int64(ct.Len()) {
			t.Fatalf("n=%d: hashes or length wrong", n)
		}
		if cLen != CipherLen(int64(n)) {
			t.Fatalf("n=%d: CipherLen %d != %d", n, CipherLen(int64(n)), cLen)
		}
		var out bytes.Buffer
		got, err := k.Decrypt(&out, bytes.NewReader(ct.Bytes()), "p/a")
		if err != nil || got != pSHA || !bytes.Equal(out.Bytes(), plain) {
			t.Fatalf("n=%d: decrypt %v", n, err)
		}
	}
}

func encryptTwoChunks(t *testing.T, k *Keys) (plain, ct []byte) {
	t.Helper()
	plain = make([]byte, ChunkSize+100)
	_, _ = rand.Read(plain)
	var buf bytes.Buffer
	if _, _, _, err := k.Encrypt(&buf, bytes.NewReader(plain), "p/two"); err != nil {
		t.Fatal(err)
	}
	return plain, buf.Bytes()
}

func mustCorrupt(t *testing.T, k *Keys, ct []byte, relpath, what string) {
	t.Helper()
	_, err := k.Decrypt(io.Discard, bytes.NewReader(ct), relpath)
	if !errors.Is(err, ErrCorrupt) {
		t.Fatalf("%s: want ErrCorrupt, got %v", what, err)
	}
}

func TestTamperingIsDetected(t *testing.T) {
	k := testKeys(t)
	_, ct := encryptTwoChunks(t, k)

	flipped := append([]byte{}, ct...)
	flipped[len(flipped)-40] ^= 0x01
	mustCorrupt(t, k, flipped, "p/two", "flipped byte")

	mustCorrupt(t, k, ct[:len(ct)-1], "p/two", "truncated tail")

	// Truncate exactly at the boundary after chunk 0 (header + one full chunk).
	mustCorrupt(t, k, ct[:21+ChunkSize+16], "p/two", "truncated at chunk boundary")

	// Swap chunk 0 and chunk 1.
	c0 := ct[21 : 21+ChunkSize+16]
	c1 := ct[21+ChunkSize+16:]
	swapped := append(append(append([]byte{}, ct[:21]...), c1...), c0...)
	mustCorrupt(t, k, swapped, "p/two", "swapped chunks")

	mustCorrupt(t, k, ct, "p/other", "wrong relpath")

	other := testKeys(t)
	mustCorrupt(t, other, ct, "p/two", "wrong key")

	bad := append([]byte{}, ct...)
	bad[0] = 'X'
	mustCorrupt(t, k, bad, "p/two", "bad magic")
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/backup/crypt/ -v`
Expected: FAIL — undefined: NewKeyHex, Keys, ChunkSize

- [ ] **Step 3: Write keys.go**

```go
// internal/backup/crypt/keys.go
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
```

- [ ] **Step 4: Write stream.go**

```go
// internal/backup/crypt/stream.go
package crypt

import (
	"bufio"
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"hash"
	"io"
)

const (
	ChunkSize  = 1 << 20
	tagSize    = 16
	saltSize   = 16
	headerSize = 4 + 1 + saltSize // magic, version, salt = 21
	formatVer  = 1
)

var magic = []byte("LSBK")

var ErrCorrupt = errors.New("crypt: object corrupt, truncated, or wrong key")

// CipherLen is the exact ciphertext length for a plaintext of plainLen
// bytes: header, one 16-byte tag per chunk, and the plaintext itself.
func CipherLen(plainLen int64) int64 {
	n := plainLen / ChunkSize
	if plainLen%ChunkSize != 0 || plainLen == 0 {
		n++
	}
	return headerSize + n*tagSize + plainLen
}

func newAEAD(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func fill(nonce, aad []byte, i uint32, last bool) {
	binary.BigEndian.PutUint32(nonce[8:], i)
	binary.BigEndian.PutUint32(aad[:4], i)
	aad[4] = 0
	if last {
		aad[4] = 1
	}
}

type countWriter struct {
	w io.Writer
	n int64
}

func (c *countWriter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	c.n += int64(n)
	return n, err
}

func hexSum(h hash.Hash) string { return hex.EncodeToString(h.Sum(nil)) }

// Encrypt streams src into dst as one object bound to relpath. It returns
// the plaintext sha256, the ciphertext sha256, and the ciphertext length,
// so the caller can sign the upload without a second pass.
func (k *Keys) Encrypt(dst io.Writer, src io.Reader, relpath string) (plainSHA, cipherSHA string, cipherLen int64, err error) {
	salt := make([]byte, saltSize)
	if _, err = rand.Read(salt); err != nil {
		return "", "", 0, err
	}
	key, err := k.objectKey(salt, relpath)
	if err != nil {
		return "", "", 0, err
	}
	aead, err := newAEAD(key)
	if err != nil {
		return "", "", 0, err
	}
	ph, ch := sha256.New(), sha256.New()
	cw := &countWriter{w: io.MultiWriter(dst, ch)}
	header := make([]byte, 0, headerSize)
	header = append(header, magic...)
	header = append(header, formatVer)
	header = append(header, salt...)
	if _, err = cw.Write(header); err != nil {
		return "", "", 0, err
	}
	br := bufio.NewReaderSize(src, ChunkSize)
	buf := make([]byte, ChunkSize)
	sealed := make([]byte, 0, ChunkSize+tagSize)
	nonce := make([]byte, 12)
	aad := make([]byte, 5)
	for i := uint32(0); ; i++ {
		n, rerr := io.ReadFull(br, buf)
		if rerr != nil && rerr != io.EOF && rerr != io.ErrUnexpectedEOF {
			return "", "", 0, rerr
		}
		chunk := buf[:n]
		ph.Write(chunk)
		last := n < ChunkSize
		if !last {
			if _, perr := br.Peek(1); perr == io.EOF {
				last = true
			} else if perr != nil {
				return "", "", 0, perr
			}
		}
		fill(nonce, aad, i, last)
		sealed = aead.Seal(sealed[:0], nonce, chunk, aad)
		if _, err = cw.Write(sealed); err != nil {
			return "", "", 0, err
		}
		if last {
			break
		}
	}
	return hexSum(ph), hexSum(ch), cw.n, nil
}

// Decrypt streams an object produced by Encrypt for relpath into dst and
// returns the plaintext sha256. Any tampering, truncation, reordering,
// wrong path, or wrong key returns ErrCorrupt.
func (k *Keys) Decrypt(dst io.Writer, src io.Reader, relpath string) (plainSHA string, err error) {
	br := bufio.NewReaderSize(src, ChunkSize+tagSize)
	header := make([]byte, headerSize)
	if _, err = io.ReadFull(br, header); err != nil {
		return "", ErrCorrupt
	}
	if !bytes.Equal(header[:4], magic) || header[4] != formatVer {
		return "", ErrCorrupt
	}
	key, err := k.objectKey(header[5:], relpath)
	if err != nil {
		return "", err
	}
	aead, err := newAEAD(key)
	if err != nil {
		return "", err
	}
	ph := sha256.New()
	buf := make([]byte, ChunkSize+tagSize)
	plain := make([]byte, 0, ChunkSize)
	nonce := make([]byte, 12)
	aad := make([]byte, 5)
	for i := uint32(0); ; i++ {
		n, rerr := io.ReadFull(br, buf)
		if rerr != nil && rerr != io.EOF && rerr != io.ErrUnexpectedEOF {
			return "", rerr
		}
		if n < tagSize {
			return "", ErrCorrupt
		}
		last := n < len(buf)
		if !last {
			if _, perr := br.Peek(1); perr == io.EOF {
				last = true
			} else if perr != nil {
				return "", perr
			}
		}
		fill(nonce, aad, i, last)
		plain, err = aead.Open(plain[:0], nonce, buf[:n], aad)
		if err != nil {
			return "", ErrCorrupt
		}
		ph.Write(plain)
		if _, err = dst.Write(plain); err != nil {
			return "", err
		}
		if last {
			break
		}
	}
	return hexSum(ph), nil
}
```

- [ ] **Step 5: Run the crypt tests**

Run: `go test ./internal/backup/crypt/ -v`
Expected: PASS (4 tests). The size loop covers empty, one byte, one chunk minus one, exactly one chunk, one chunk plus one, and three chunks plus seven.

- [ ] **Step 6: gofmt, vet, commit**

```bash
gofmt -l internal/backup && go vet ./internal/backup/... && \
git add internal/backup/crypt && \
git commit -m "Add backup crypt: per-object HKDF keys and chunked AES-GCM."
```

---

### Task 4: store.Snapshot, backup config, key, state cache, lock

**Files:**
- Create: `internal/store/snapshot.go`
- Test: `internal/store/snapshot_test.go`
- Create: `internal/backup/config.go`
- Create: `internal/backup/state.go`
- Create: `internal/backup/manifest.go` (types and generation ids only; network I/O comes in Task 5)
- Create: `internal/backup/lock.go`
- Test: `internal/backup/config_test.go`
- Test: `internal/backup/state_test.go`
- Test: `internal/backup/lock_test.go`

**Interfaces:**
- Consumes: `s3.ParseURL`, `s3.Config` (Task 1); `crypt.NewKeyHex`, `crypt.ParseKeyHex` (Task 3); `store.sqliteURI` (unexported, same package as `Snapshot`).
- Produces:
  - `func store.Snapshot(src, dst string) error`
  - `type Config struct { URL string; S3 s3.Config; Every time.Duration; Keep int }`
  - `var ErrNotConfigured`
  - `type ConfigError struct { Err error }` — wraps any failure before the first network call (missing env or key, bad URL or endpoint); the CLI exits 2 on it
  - `func LoadConfig(home string) (*Config, error)`
  - `type InitOptions struct { URL, Endpoint, Region string; Every time.Duration; Keep int }`
  - `func Init(home string, o InitOptions) error`
  - `func LoadKey(home string) (*crypt.Keys, error)`
  - `type FileState struct { Size, StatSize, Mtime int64; SHA256, Object string }` — `StatSize` is the on-disk size when hashed (main plus `-wal` for sqlite); `Size` is the bytes that go in the manifest
  - `type State struct { Files map[string]FileState; LastOK, LastAttempt, LastError, LastGeneration string; Adopted []string; Manifests map[string]*Manifest }`
  - `func LoadState(home string) *State`, `func (s *State) Save(home string) error`, `func (s *State) LastOKTime() (time.Time, bool)`, `func (s *State) Adopt(client string)`, `func (s *State) AllowsWriter(me, client string) bool`
  - `type FileEntry struct { SHA256 string; Size int64; Object string }`
  - `type Manifest struct { Version int; Generation, CreatedAt, Lossless, Client string; Generations, Dropping []string; Files map[string]FileEntry }`
  - `func NewGeneration(now time.Time) string` — `20060102T150405Z-xxxx`
  - `var ErrLocked`, `func Lock(home string) (release func(), err error)`

- [ ] **Step 1: Write the failing snapshot test**

```go
// internal/store/snapshot_test.go
package store

import (
	"database/sql"
	"path/filepath"
	"testing"

	"lossless/internal/claim"
)

func TestSnapshotSeesUncheckpointedWrites(t *testing.T) {
	st := tmp(t)
	if _, err := st.WriteClaim(claim.Record{Type: "decision", Text: "Use jose for Edge.", ProjectKey: "acme/api"}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.WriteClaim(claim.Record{Type: "failed", Text: "Redis limiter blew the p95.", ProjectKey: "acme/api"}); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(t.TempDir(), "claims.sqlite")
	if err := Snapshot(filepath.Join(st.Root, "index", "claims.sqlite"), dst); err != nil {
		t.Fatal(err)
	}
	// A second call replaces the file instead of failing on "already exists".
	if err := Snapshot(filepath.Join(st.Root, "index", "claims.sqlite"), dst); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", sqliteURI(dst))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM records`).Scan(&n); err != nil || n != 2 {
		t.Fatalf("records in snapshot: %d %v", n, err)
	}
	var mode string
	if err := db.QueryRow(`PRAGMA journal_mode`).Scan(&mode); err != nil {
		t.Fatal(err)
	}
	// The live store keeps working after the snapshot.
	active, err := st.ListActive("acme/api")
	if err != nil || len(active) != 2 {
		t.Fatalf("live store: %d %v", len(active), err)
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/store/ -run TestSnapshot -v`
Expected: FAIL — undefined: Snapshot

- [ ] **Step 3: Write snapshot.go**

```go
// internal/store/snapshot.go
package store

import (
	"database/sql"
	"fmt"
	"os"
	"strings"
)

// Snapshot writes a consistent single-file copy of the sqlite database at
// src to dst with VACUUM INTO on a fresh read connection. The daemon's
// WAL stays live and is not checkpointed. dst is replaced if present.
func Snapshot(src, dst string) error {
	if err := os.Remove(dst); err != nil && !os.IsNotExist(err) {
		return err
	}
	db, err := sql.Open("sqlite", sqliteURI(src))
	if err != nil {
		return err
	}
	defer db.Close()
	quoted := "'" + strings.ReplaceAll(dst, "'", "''") + "'"
	if _, err := db.Exec("VACUUM INTO " + quoted); err != nil {
		return fmt.Errorf("snapshot %s: %w", src, err)
	}
	return os.Chmod(dst, 0o600)
}
```

- [ ] **Step 4: Run the snapshot test**

Run: `go test ./internal/store/ -run TestSnapshot -v`
Expected: PASS

- [ ] **Step 5: Write the failing backup config, state, and lock tests**

```go
// internal/backup/config_test.go
package backup

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func clearBackupEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{"LOSSLESS_BACKUP_URL", "LOSSLESS_BACKUP_ENDPOINT", "LOSSLESS_BACKUP_REGION",
		"LOSSLESS_BACKUP_ACCESS_KEY", "LOSSLESS_BACKUP_SECRET_KEY", "LOSSLESS_BACKUP_EVERY", "LOSSLESS_BACKUP_KEEP",
		"AWS_ACCESS_KEY_ID", "AWS_SECRET_ACCESS_KEY", "AWS_SESSION_TOKEN"} {
		t.Setenv(k, "")
	}
}

func TestLoadConfigNotConfigured(t *testing.T) {
	clearBackupEnv(t)
	_, err := LoadConfig(t.TempDir())
	if !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("want ErrNotConfigured, got %v", err)
	}
}

func TestInitThenLoad(t *testing.T) {
	clearBackupEnv(t)
	t.Setenv("LOSSLESS_BACKUP_ACCESS_KEY", "AK")
	t.Setenv("LOSSLESS_BACKUP_SECRET_KEY", "SK")
	home := t.TempDir()
	err := Init(home, InitOptions{URL: "s3://bkt/pre", Endpoint: "https://acct.r2.cloudflarestorage.com", Every: time.Hour, Keep: 5})
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"backup.env", "backup.key"} {
		st, err := os.Stat(filepath.Join(home, f))
		if err != nil || st.Mode().Perm() != 0o600 {
			t.Fatalf("%s: %v mode=%v", f, err, st.Mode())
		}
	}
	// Credentials present at init time are written into backup.env so the
	// daemon can read them later without the shell environment.
	clearBackupEnv(t)
	cfg, err := LoadConfig(home)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.S3.Bucket != "bkt" || cfg.S3.Prefix != "pre" || cfg.S3.Endpoint != "https://acct.r2.cloudflarestorage.com" ||
		cfg.S3.AccessKey != "AK" || cfg.S3.SecretKey != "SK" || cfg.Every != time.Hour || cfg.Keep != 5 {
		t.Fatalf("%+v", cfg)
	}
	if _, err := LoadKey(home); err != nil {
		t.Fatal(err)
	}
	// A second init must not overwrite the key.
	if err := Init(home, InitOptions{URL: "s3://other"}); err == nil || !strings.Contains(err.Error(), "backup.key") {
		t.Fatalf("second init must refuse: %v", err)
	}
}

func TestInitWithoutCredentialsLeavesPlaceholders(t *testing.T) {
	clearBackupEnv(t)
	home := t.TempDir()
	if err := Init(home, InitOptions{URL: "s3://bkt"}); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(home, "backup.env"))
	if !strings.Contains(string(b), "# LOSSLESS_BACKUP_ACCESS_KEY=") {
		t.Fatalf("placeholder missing:\n%s", b)
	}
	_, err := LoadConfig(home)
	if err == nil || !strings.Contains(err.Error(), "credentials") {
		t.Fatalf("want credentials error, got %v", err)
	}
	// AWS_* in the environment is an accepted fallback.
	t.Setenv("AWS_ACCESS_KEY_ID", "A")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "S")
	t.Setenv("AWS_SESSION_TOKEN", "T")
	cfg, err := LoadConfig(home)
	if err != nil || cfg.S3.AccessKey != "A" || cfg.S3.SessionToken != "T" {
		t.Fatalf("%+v %v", cfg, err)
	}
}

func TestInitDefaultsAndDisabledSchedule(t *testing.T) {
	clearBackupEnv(t)
	home := t.TempDir()
	if err := Init(home, InitOptions{URL: "s3://bkt", Every: 0}); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(home, "backup.env"))
	if !strings.Contains(string(b), `LOSSLESS_BACKUP_EVERY="0"`) || !strings.Contains(string(b), `LOSSLESS_BACKUP_KEEP="5"`) {
		t.Fatalf("defaults:\n%s", b)
	}
	t.Setenv("LOSSLESS_BACKUP_ACCESS_KEY", "A")
	t.Setenv("LOSSLESS_BACKUP_SECRET_KEY", "S")
	cfg, err := LoadConfig(home)
	if err != nil || cfg.Every != 0 || cfg.Keep != 5 {
		t.Fatalf("%+v %v", cfg, err)
	}
	if err := Init(t.TempDir(), InitOptions{URL: "nope"}); err == nil {
		t.Fatal("bad URL must be refused")
	}
}
```

```go
// internal/backup/state_test.go
package backup

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestStateRoundTrip(t *testing.T) {
	home := t.TempDir()
	s := LoadState(home)
	if len(s.Files) != 0 || s.LastOK != "" {
		t.Fatalf("fresh state: %+v", s)
	}
	s.Files["raw/a.jsonl.zst"] = FileState{Size: 3, Mtime: 4, SHA256: "abc", Object: "o/n/abc"}
	s.LastOK = time.Date(2026, 9, 14, 1, 2, 3, 0, time.UTC).Format(time.RFC3339)
	s.Adopt("c-1")
	s.Adopt("c-1")
	s.Manifests["g1"] = &Manifest{Generation: "g1", Files: map[string]FileEntry{"x": {SHA256: "s", Size: 1, Object: "o"}}}
	if err := s.Save(home); err != nil {
		t.Fatal(err)
	}
	st, _ := os.Stat(filepath.Join(home, "backup-state.json"))
	if st.Mode().Perm() != 0o600 {
		t.Fatal(st.Mode())
	}
	got := LoadState(home)
	if got.Files["raw/a.jsonl.zst"].SHA256 != "abc" || len(got.Adopted) != 1 || got.Manifests["g1"].Files["x"].Size != 1 {
		t.Fatalf("%+v", got)
	}
	ok, has := got.LastOKTime()
	if !has || ok.Hour() != 1 {
		t.Fatal(ok, has)
	}
	if !got.AllowsWriter("me", "") || !got.AllowsWriter("me", "me") || !got.AllowsWriter("me", "c-1") || got.AllowsWriter("me", "c-2") {
		t.Fatal("AllowsWriter")
	}
}

func TestStateCorruptFileIsEmpty(t *testing.T) {
	home := t.TempDir()
	_ = os.WriteFile(filepath.Join(home, "backup-state.json"), []byte("{not json"), 0o600)
	if s := LoadState(home); len(s.Files) != 0 || s.Files == nil {
		t.Fatalf("%+v", s)
	}
}

func TestNewGenerationShape(t *testing.T) {
	g := NewGeneration(time.Date(2026, 9, 12, 20, 15, 0, 0, time.UTC))
	if len(g) != len("20260912T201500Z-a1f3") || g[:16] != "20260912T201500Z" || g[16] != '-' {
		t.Fatal(g)
	}
	if NewGeneration(time.Now()) == NewGeneration(time.Now()) {
		t.Fatal("random suffix must differ")
	}
}
```

```go
// internal/backup/lock_test.go
package backup

import (
	"errors"
	"testing"
)

func TestLockIsExclusive(t *testing.T) {
	home := t.TempDir()
	release, err := Lock(home)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Lock(home); !errors.Is(err, ErrLocked) {
		t.Fatalf("want ErrLocked, got %v", err)
	}
	release()
	release2, err := Lock(home)
	if err != nil {
		t.Fatal(err)
	}
	release2()
}
```

- [ ] **Step 6: Run them to verify they fail**

Run: `go test ./internal/backup/ -v`
Expected: FAIL — undefined: LoadConfig, Init, LoadState, Lock, Manifest

- [ ] **Step 7: Write config.go**

```go
// internal/backup/config.go
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
// shape service.env uses. Blank lines and # comments are skipped.
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
		v = strings.TrimSpace(v)
		if strings.HasPrefix(v, `"`) {
			if u, err := strconv.Unquote(v); err == nil {
				v = u
			}
		}
		out[strings.TrimSpace(k)] = v
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
	keyHex, err := crypt.NewKeyHex()
	if err != nil {
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
```

- [ ] **Step 8: Write manifest.go (types only) and state.go**

```go
// internal/backup/manifest.go
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
```

```go
// internal/backup/state.go
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
```

- [ ] **Step 9: Write lock.go**

```go
// internal/backup/lock.go
package backup

import (
	"errors"
	"os"
	"syscall"
)

var ErrLocked = errors.New("backup already running")

// Lock takes an exclusive, non-blocking flock on <home>/backup.lock so the
// daemon's scheduled run and a manual command never overlap.
func Lock(home string) (release func(), err error) {
	if err := os.MkdirAll(home, 0o700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(lockPath(home), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		return nil, ErrLocked
	}
	return func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		_ = f.Close()
	}, nil
}
```

- [ ] **Step 10: Run the backup package tests**

Run: `go test ./internal/backup/ ./internal/store/ -v`
Expected: PASS (config 4, state 3, lock 1, plus the existing store tests)

- [ ] **Step 11: gofmt, vet, commit**

```bash
gofmt -l internal && go vet ./... && \
git add internal/store/snapshot.go internal/store/snapshot_test.go internal/backup && \
git commit -m "Add store.Snapshot and backup config, key, state cache, lock."
```

---

### Task 5: manifest transport and the walk

**Files:**
- Modify: `internal/backup/manifest.go` (add the encrypted get/put)
- Create: `internal/backup/walk.go`
- Test: `internal/backup/manifest_test.go`
- Test: `internal/backup/walk_test.go`

**Interfaces:**
- Consumes: `s3.Client` (Task 2), `crypt.Keys` (Task 3), `store.Snapshot`, `Manifest`, `State`, `objectKey` (Task 4), `write.SealRaw` (existing, for the test fixture).
- Produces:
  - `type remote struct { c *s3.Client; keys *crypt.Keys }`
  - `func (r *remote) getManifest(ctx context.Context, key string) (*Manifest, error)` — `s3.ErrNotFound` passes through; `ErrWrongKey` when decryption fails
  - `func (r *remote) putManifest(ctx context.Context, key string, m *Manifest) error`
  - `var ErrWrongKey`
  - `type Item struct { Rel, Src string; Size, StatSize, Mtime int64; SHA256, Object string; Temp bool }`
  - `func walk(home, tmp string, keys *crypt.Keys, cache *State) ([]Item, error)`
  - `func hashFile(path string) (string, int64, error)`

- [ ] **Step 1: Write the failing manifest transport test**

```go
// internal/backup/manifest_test.go
package backup

import (
	"context"
	"errors"
	"testing"
	"time"

	"lossless/internal/backup/crypt"
	"lossless/internal/backup/s3"
	"lossless/internal/backup/s3/s3test"
)

func testRemote(t *testing.T, srv *s3test.Server) *remote {
	t.Helper()
	c, err := s3.New(srv.Config("bkt", "pre"))
	if err != nil {
		t.Fatal(err)
	}
	c.SetSleep(func(d time.Duration) {})
	h, _ := crypt.NewKeyHex()
	k, _ := crypt.ParseKeyHex(h)
	return &remote{c: c, keys: k}
}

func TestManifestRoundTripAndWrongKey(t *testing.T) {
	srv := s3test.New()
	defer srv.Close()
	r := testRemote(t, srv)
	ctx := context.Background()
	if _, err := r.getManifest(ctx, pointerKey); !errors.Is(err, s3.ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
	m := emptyManifest()
	m.Generation = "g1"
	m.Client = "c-1"
	m.Generations = []string{"g1"}
	m.Files["export/p/x.md"] = FileEntry{SHA256: "s", Size: 2, Object: "o/n/s"}
	if err := r.putManifest(ctx, pointerKey, m); err != nil {
		t.Fatal(err)
	}
	got, err := r.getManifest(ctx, pointerKey)
	if err != nil || got.Generation != "g1" || got.Files["export/p/x.md"].Size != 2 || got.Client != "c-1" {
		t.Fatalf("%+v %v", got, err)
	}
	if b, ok := srv.Object("bkt", "pre/manifest"); !ok || string(b[:4]) != "LSBK" {
		t.Fatal("manifest must be stored encrypted")
	}
	other := testRemote(t, srv)
	if _, err := other.getManifest(ctx, pointerKey); !errors.Is(err, ErrWrongKey) {
		t.Fatalf("want ErrWrongKey, got %v", err)
	}
}
```


- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/backup/ -run TestManifest -v`
Expected: FAIL — undefined: remote, ErrWrongKey

- [ ] **Step 3: Append the transport to manifest.go**

```go
// append to internal/backup/manifest.go; extend the import block with
// "bytes", "context", "encoding/json", "errors", "fmt",
// "lossless/internal/backup/crypt", "lossless/internal/backup/s3"

var ErrWrongKey = errors.New("backup.key does not match this bucket")

// remote is a bucket plus the key that encrypts everything in it.
type remote struct {
	c    *s3.Client
	keys *crypt.Keys
}

func (r *remote) getManifest(ctx context.Context, key string) (*Manifest, error) {
	body, _, err := r.c.Get(ctx, key)
	if err != nil {
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
```

- [ ] **Step 4: Run the manifest test**

Run: `go test ./internal/backup/ -run TestManifest -v`
Expected: PASS

- [ ] **Step 5: Write the failing walk tests**

```go
// internal/backup/walk_test.go
package backup

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"lossless/internal/backup/crypt"
	"lossless/internal/claim"
	"lossless/internal/store"
	"lossless/internal/write"
)

func testKeys(t *testing.T) *crypt.Keys {
	t.Helper()
	h, _ := crypt.NewKeyHex()
	k, err := crypt.ParseKeyHex(h)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

// seedHome builds a store with two claims, one sealed part, one live part,
// and every kind of file the walk must skip.
func seedHome(t *testing.T) (string, *store.Store) {
	t.Helper()
	home := t.TempDir()
	st, err := store.Open(home)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	for _, txt := range []string{"Use jose, not jsonwebtoken, for Edge.", "Redis limiter blew the p95 budget."} {
		if _, err := st.WriteClaim(claim.Record{Type: "decision", Text: txt, ProjectKey: "acme/api"}); err != nil {
			t.Fatal(err)
		}
	}
	rawDir := filepath.Join(home, "raw", "acme__api", "2026-09")
	must(t, os.MkdirAll(rawDir, 0o700))
	must(t, os.WriteFile(filepath.Join(rawDir, "sealed.jsonl"), []byte(`{"role":"user","content":"sealed"}`+"\n"), 0o600))
	if _, err := write.SealRaw(filepath.Join(rawDir, "sealed.jsonl")); err != nil {
		t.Fatal(err)
	}
	must(t, os.WriteFile(filepath.Join(rawDir, "live.jsonl"), []byte(`{"role":"user","content":"live"}`+"\n"), 0o600))
	must(t, os.WriteFile(filepath.Join(rawDir, "live.jsonl.lock"), nil, 0o600))
	must(t, os.WriteFile(filepath.Join(rawDir, "junk.jsonl.zst.tmp"), []byte("x"), 0o600))
	must(t, os.WriteFile(filepath.Join(home, "export", "acme__api", "stale.md.tmp"), []byte("x"), 0o600))
	must(t, os.Symlink(filepath.Join(rawDir, "live.jsonl"), filepath.Join(home, "export", "acme__api", "link.md")))
	must(t, os.WriteFile(filepath.Join(home, "index", "excerpts-2026-07.sqlite"), nil, 0o600))
	must(t, copyFile(filepath.Join(home, "index", "claims.sqlite"), filepath.Join(home, "index", "excerpts-2026-08.sqlite")))
	must(t, os.WriteFile(filepath.Join(home, "service.env"), []byte("LOSSLESS_HOME=x\n"), 0o600))
	must(t, os.WriteFile(filepath.Join(home, "spool", "push-1.json"), []byte("{}"), 0o600))
	return home, st
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func copyFile(src, dst string) error {
	b, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, b, 0o600)
}

func rels(items []Item) []string {
	var out []string
	for _, it := range items {
		out = append(out, it.Rel)
	}
	sort.Strings(out)
	return out
}

func TestWalkSelectsExactlyTheStore(t *testing.T) {
	home, _ := seedHome(t)
	tmp := filepath.Join(home, "backup-tmp")
	items, err := walk(home, tmp, testKeys(t), LoadState(home))
	if err != nil {
		t.Fatal(err)
	}
	got := rels(items)
	var want []string
	for _, r := range []string{"raw/acme__api/2026-09/sealed.jsonl.zst", "raw/acme__api/2026-09/live.jsonl", "index/claims.sqlite", "index/excerpts-2026-08.sqlite"} {
		want = append(want, r)
	}
	mds, _ := filepath.Glob(filepath.Join(home, "export", "acme__api", "*.md"))
	for _, m := range mds {
		if strings.HasSuffix(m, "link.md") {
			continue
		}
		want = append(want, "export/acme__api/"+filepath.Base(m))
	}
	sort.Strings(want)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("\n got %v\nwant %v", got, want)
	}
	for _, it := range items {
		if it.SHA256 == "" || !strings.HasPrefix(it.Object, "o/") || !strings.HasSuffix(it.Object, "/"+it.SHA256) {
			t.Fatalf("item %+v", it)
		}
		switch {
		case strings.HasSuffix(it.Rel, "live.jsonl"), strings.HasSuffix(it.Rel, ".sqlite"):
			if !it.Temp || !strings.HasPrefix(it.Src, tmp) {
				t.Fatalf("live and sqlite must be temp copies: %+v", it)
			}
			b, _ := os.ReadFile(it.Src)
			if strings.HasSuffix(it.Rel, "live.jsonl") && string(b) != `{"role":"user","content":"live"}`+"\n" {
				t.Fatalf("live copy content: %q", b)
			}
		default:
			if it.Temp || !strings.HasPrefix(it.Src, home) {
				t.Fatalf("direct read expected: %+v", it)
			}
		}
	}
}

func TestWalkReusesCachedHashWhenStatUnchanged(t *testing.T) {
	home, _ := seedHome(t)
	tmp := filepath.Join(home, "backup-tmp")
	k := testKeys(t)
	cache := LoadState(home)
	items, err := walk(home, tmp, k, cache)
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range items {
		cache.Files[it.Rel] = FileState{Size: it.Size, StatSize: it.StatSize, Mtime: it.Mtime, SHA256: it.SHA256, Object: it.Object}
	}
	sealed := filepath.Join(home, "raw", "acme__api", "2026-09", "sealed.jsonl.zst")
	st, _ := os.Stat(sealed)
	b, _ := os.ReadFile(sealed)
	b[len(b)-1] ^= 0xff // same size, different bytes
	must(t, os.WriteFile(sealed, b, 0o600))
	must(t, os.Chtimes(sealed, st.ModTime(), st.ModTime()))
	again, err := walk(home, tmp, k, cache)
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range again {
		if it.Rel == "raw/acme__api/2026-09/sealed.jsonl.zst" && it.SHA256 != cache.Files[it.Rel].SHA256 {
			t.Fatal("unchanged size+mtime must reuse the cached hash")
		}
	}
	must(t, os.Chtimes(sealed, time.Now(), time.Now()))
	third, _ := walk(home, tmp, k, cache)
	for _, it := range third {
		if it.Rel == "raw/acme__api/2026-09/sealed.jsonl.zst" && it.SHA256 == cache.Files[it.Rel].SHA256 {
			t.Fatal("a new mtime must re-hash")
		}
	}
}

func TestLiveItemFallsBackToSealedSibling(t *testing.T) {
	home, _ := seedHome(t)
	tmp := filepath.Join(home, "backup-tmp")
	must(t, os.MkdirAll(tmp, 0o700))
	k := testKeys(t)
	// The walk saw live.jsonl, then the watcher sealed it before the copy.
	live := filepath.Join(home, "raw", "acme__api", "2026-09", "live.jsonl")
	if _, err := write.SealRaw(live); err != nil {
		t.Fatal(err)
	}
	it, err := liveItem(home, tmp, "raw/acme__api/2026-09/live.jsonl", k, LoadState(home))
	if err != nil || it.Rel != "raw/acme__api/2026-09/live.jsonl.zst" || it.Temp {
		t.Fatalf("%+v %v", it, err)
	}
}
```

- [ ] **Step 6: Run them to verify they fail**

Run: `go test ./internal/backup/ -run 'TestWalk|TestLiveItem' -v`
Expected: FAIL — undefined: walk, Item, liveItem

- [ ] **Step 7: Write walk.go**

```go
// internal/backup/walk.go
package backup

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"lossless/internal/backup/crypt"
	"lossless/internal/store"
)

// Item is one file the backup wants in the bucket.
type Item struct {
	Rel      string // relative to home, forward slashes
	Src      string // path to read: the original, or a copy under backup-tmp
	Size     int64  // bytes that go in the manifest
	StatSize int64  // on-disk size when hashed; sqlite: main + -wal
	Mtime    int64  // cache key of the original (ns); sqlite: max of main and -wal
	SHA256   string
	Object   string // o/<name>/<sha256>
	Temp     bool   // Src is under backup-tmp
}

var roots = []string{"raw", "export", "index"}

func skipName(name string) bool {
	for _, suf := range []string{".lock", ".tmp", "-wal", "-shm"} {
		if strings.HasSuffix(name, suf) {
			return true
		}
	}
	return false
}

// walk lists everything under raw/, export/, and index/ that belongs in a
// backup. Live parts and sqlite files are copied to tmp first; sealed parts
// and claim files are read in place. cache short-circuits hashing when
// size and mtime match.
func walk(home, tmp string, keys *crypt.Keys, cache *State) ([]Item, error) {
	if err := os.MkdirAll(tmp, 0o700); err != nil {
		return nil, err
	}
	var items []Item
	for _, root := range roots {
		dir := filepath.Join(home, root)
		if _, err := os.Stat(dir); err != nil {
			continue
		}
		err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || d.Type()&fs.ModeSymlink != 0 || skipName(d.Name()) {
				return nil
			}
			rel, err := filepath.Rel(home, path)
			if err != nil {
				return err
			}
			rel = filepath.ToSlash(rel)
			var it Item
			switch {
			case root == "raw" && strings.HasSuffix(rel, ".jsonl.zst"):
				it, err = directItem(home, rel, keys, cache)
			case root == "raw" && strings.HasSuffix(rel, ".jsonl"):
				it, err = liveItem(home, tmp, rel, keys, cache)
			case root == "export" && strings.HasSuffix(rel, ".md"):
				it, err = directItem(home, rel, keys, cache)
			case root == "index" && (d.Name() == "claims.sqlite" ||
				(strings.HasPrefix(d.Name(), "excerpts-") && strings.HasSuffix(d.Name(), ".sqlite"))):
				it, err = sqliteItem(home, tmp, rel, keys, cache)
			default:
				return nil
			}
			if err != nil {
				return fmt.Errorf("%s: %w", rel, err)
			}
			if it.Rel != "" {
				items = append(items, it)
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	return items, nil
}

func hashFile(path string) (string, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(h.Sum(nil)), n, nil
}

func finish(it Item, keys *crypt.Keys) Item {
	it.Object = objectKey(keys.Name(it.Rel), it.SHA256)
	return it
}

// directItem reads an immutable or atomically-replaced file in place.
func directItem(home, rel string, keys *crypt.Keys, cache *State) (Item, error) {
	path := filepath.Join(home, filepath.FromSlash(rel))
	st, err := os.Stat(path)
	if err != nil {
		return Item{}, err
	}
	it := Item{Rel: rel, Src: path, Size: st.Size(), StatSize: st.Size(), Mtime: st.ModTime().UnixNano()}
	if c, ok := cache.Files[rel]; ok && c.StatSize == it.StatSize && c.Mtime == it.Mtime && c.SHA256 != "" {
		it.SHA256 = c.SHA256
		return finish(it, keys), nil
	}
	sha, n, err := hashFile(path)
	if err != nil {
		return Item{}, err
	}
	it.SHA256, it.Size = sha, n
	return finish(it, keys), nil
}

// liveItem copies a live part to tmp under shared flocks on the part and
// its .lock sidecar, then hashes the copy. If the part was sealed between
// the walk and the open, the .zst sibling is used in place instead.
func liveItem(home, tmp, rel string, keys *crypt.Keys, cache *State) (Item, error) {
	path := filepath.Join(home, filepath.FromSlash(rel))
	st, err := os.Stat(path)
	if os.IsNotExist(err) {
		if _, zerr := os.Stat(path + ".zst"); zerr == nil {
			return directItem(home, rel+".zst", keys, cache)
		}
		return Item{}, nil
	}
	if err != nil {
		return Item{}, err
	}
	it := Item{Rel: rel, Size: st.Size(), StatSize: st.Size(), Mtime: st.ModTime().UnixNano()}
	if c, ok := cache.Files[rel]; ok && c.StatSize == it.StatSize && c.Mtime == it.Mtime && c.SHA256 != "" {
		// Unchanged since the last run; the object is already in the bucket
		// (or will be re-planned from the pointer), so no copy is needed.
		it.SHA256 = c.SHA256
		it.Src = path
		return finish(it, keys), nil
	}
	dst := filepath.Join(tmp, keys.Name(rel)+".live")
	if err := copyLive(path, dst); err != nil {
		if os.IsNotExist(err) {
			if _, zerr := os.Stat(path + ".zst"); zerr == nil {
				return directItem(home, rel+".zst", keys, cache)
			}
			return Item{}, nil
		}
		return Item{}, err
	}
	sha, n, err := hashFile(dst)
	if err != nil {
		return Item{}, err
	}
	it.Src, it.SHA256, it.Size, it.Temp = dst, sha, n, true
	return finish(it, keys), nil
}

// copyLive holds LOCK_SH on <part>.lock (catch-up's lock) and on the part
// itself (append's lock) only for the local copy. Both writers hold their
// exclusive lock for one append and fsync, so this waits microseconds and
// never makes a hook wait on the network.
func copyLive(src, dst string) error {
	lockFile, err := os.OpenFile(src+".lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	defer lockFile.Close()
	if err := syscall.Flock(int(lockFile.Fd()), syscall.LOCK_SH); err != nil {
		return err
	}
	defer func() { _ = syscall.Flock(int(lockFile.Fd()), syscall.LOCK_UN) }()
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := syscall.Flock(int(in.Fd()), syscall.LOCK_SH); err != nil {
		return err
	}
	defer func() { _ = syscall.Flock(int(in.Fd()), syscall.LOCK_UN) }()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}

// sqliteItem snapshots a database with VACUUM INTO. The cache key combines
// the main file and its -wal so an uncheckpointed write still re-snapshots.
func sqliteItem(home, tmp, rel string, keys *crypt.Keys, cache *State) (Item, error) {
	path := filepath.Join(home, filepath.FromSlash(rel))
	st, err := os.Stat(path)
	if err != nil {
		return Item{}, err
	}
	if st.Size() == 0 {
		return Item{}, nil
	}
	size, mtime := st.Size(), st.ModTime().UnixNano()
	if wst, err := os.Stat(path + "-wal"); err == nil {
		size += wst.Size()
		if m := wst.ModTime().UnixNano(); m > mtime {
			mtime = m
		}
	}
	it := Item{Rel: rel, Mtime: mtime, StatSize: size}
	if c, ok := cache.Files[rel]; ok && c.Mtime == mtime && c.StatSize == size && c.SHA256 != "" {
		it.SHA256, it.Size, it.Src = c.SHA256, c.Size, path
		return finish(it, keys), nil
	}
	dst := filepath.Join(tmp, keys.Name(rel)+".sqlite")
	if err := store.Snapshot(path, dst); err != nil {
		return Item{}, err
	}
	sha, n, err := hashFile(dst)
	if err != nil {
		return Item{}, err
	}
	it.Src, it.SHA256, it.Size, it.Temp = dst, sha, n, true
	it.Mtime = mtime
	return finish(it, keys), nil
}
```

- [ ] **Step 8: Run the walk tests**

Run: `go test ./internal/backup/ -run 'TestWalk|TestLiveItem|TestManifest' -v`
Expected: PASS

- [ ] **Step 9: gofmt, vet, commit**

```bash
gofmt -l internal && go vet ./... && \
git add internal/backup && \
git commit -m "Add backup manifest transport and the store walk."
```

---

### Task 6: plan and Run (backup with generations and the writer guard)

**Files:**
- Create: `internal/backup/plan.go`
- Create: `internal/backup/run.go`
- Test: `internal/backup/plan_test.go`
- Test: `internal/backup/run_test.go`

**Interfaces:**
- Consumes: `walk`, `Item`, `remote`, `getManifest`, `putManifest` (Task 5); `LoadConfig`, `LoadKey`, `Lock`, `LoadState`, `Manifest`, `NewGeneration`, `manifestKey`, `pointerKey` (Task 4); `s3.New`, `s3.ErrNotFound` (Task 2); `write.ClientID(home string) string` (existing); `version.Version` (existing).
- Produces:
  - `type plan struct { Files map[string]FileEntry; Upload []Item; Removed []string; Kept []string; Drop []string; NoChange bool }`
  - `func makePlan(items []Item, pointer *Manifest, keep int, gen string) plan` — `Drop` holds only generations newly pushed out by this run; the pointer's pending `Dropping` is handled by `Run`
  - `type RunOptions struct { DryRun, Verbose, TakeOver bool; Out io.Writer; Now func() time.Time }`
  - `type Summary struct { Scanned, Uploaded, Deleted int; Bytes int64; Generation string; NoChange, DryRun bool; Dropped []string; Elapsed time.Duration }`
  - `type OtherWriterError struct { Client, CreatedAt string }`
  - `func Run(ctx context.Context, home string, o RunOptions) (Summary, error)`
  - `var sleepHook func(time.Duration)` — nil means real sleep; tests set it
  - `func filesFrom(items []Item) map[string]FileEntry`

- [ ] **Step 1: Write the failing plan test**

```go
// internal/backup/plan_test.go
package backup

import (
	"strings"
	"testing"
)

func item(rel, sha string) Item {
	return Item{Rel: rel, SHA256: sha, Size: 1, Object: "o/n-" + rel + "/" + sha}
}

func TestMakePlanDiffsAgainstPointer(t *testing.T) {
	pointer := emptyManifest()
	pointer.Generations = []string{"g2", "g1"}
	pointer.Files["a"] = FileEntry{SHA256: "1", Object: "o/n-a/1"}
	pointer.Files["b"] = FileEntry{SHA256: "1", Object: "o/n-b/1"}
	pointer.Files["gone"] = FileEntry{SHA256: "1", Object: "o/n-gone/1"}
	p := makePlan([]Item{item("a", "1"), item("b", "2"), item("new", "1")}, pointer, 5, "g3")
	if p.NoChange || len(p.Upload) != 2 || p.Upload[0].Rel != "b" || p.Upload[1].Rel != "new" {
		t.Fatalf("%+v", p)
	}
	if strings.Join(p.Removed, ",") != "gone" {
		t.Fatalf("removed %v", p.Removed)
	}
	if strings.Join(p.Kept, ",") != "g3,g2,g1" || len(p.Drop) != 0 {
		t.Fatalf("kept %v drop %v", p.Kept, p.Drop)
	}
	if p.Files["b"].SHA256 != "2" || len(p.Files) != 3 {
		t.Fatalf("files %+v", p.Files)
	}
}

func TestMakePlanNoChangeAndKeep(t *testing.T) {
	pointer := emptyManifest()
	pointer.Generations = []string{"g3", "g2", "g1"}
	pointer.Dropping = []string{"g0"}
	pointer.Files["a"] = FileEntry{SHA256: "1", Object: "o/n-a/1"}
	p := makePlan([]Item{item("a", "1")}, pointer, 2, "g4")
	if !p.NoChange || strings.Join(p.Kept, ",") != "g3,g2,g1" || len(p.Drop) != 0 {
		t.Fatalf("no change must keep the pointer's generations: %+v", p)
	}
	p = makePlan([]Item{item("a", "2")}, pointer, 2, "g4")
	if p.NoChange || strings.Join(p.Kept, ",") != "g4,g3" || strings.Join(p.Drop, ",") != "g2,g1" {
		t.Fatalf("keep=2: kept %v drop %v", p.Kept, p.Drop)
	}
	// A generation already under Dropping is never re-kept.
	pointer.Generations = []string{"g3", "g0"}
	p = makePlan([]Item{item("a", "2")}, pointer, 5, "g4")
	if strings.Join(p.Kept, ",") != "g4,g3" {
		t.Fatalf("dropping excluded: %v", p.Kept)
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/backup/ -run TestMakePlan -v`
Expected: FAIL — undefined: makePlan

- [ ] **Step 3: Write plan.go**

```go
// internal/backup/plan.go
package backup

import "sort"

type plan struct {
	Files    map[string]FileEntry // desired set
	Upload   []Item               // (rel, sha) the pointer does not hold
	Removed  []string             // rels the pointer holds that are gone
	Kept     []string             // generations kept after this run, newest first
	Drop     []string             // generations this run pushes out (not the pointer's pending Dropping)
	NoChange bool
}

func filesFrom(items []Item) map[string]FileEntry {
	out := make(map[string]FileEntry, len(items))
	for _, it := range items {
		out[it.Rel] = FileEntry{SHA256: it.SHA256, Size: it.Size, Object: it.Object}
	}
	return out
}

func makePlan(items []Item, pointer *Manifest, keep int, gen string) plan {
	p := plan{Files: filesFrom(items)}
	for _, it := range items {
		if prev, ok := pointer.Files[it.Rel]; !ok || prev.SHA256 != it.SHA256 {
			p.Upload = append(p.Upload, it)
		}
	}
	sort.Slice(p.Upload, func(i, j int) bool { return p.Upload[i].Rel < p.Upload[j].Rel })
	for rel := range pointer.Files {
		if _, ok := p.Files[rel]; !ok {
			p.Removed = append(p.Removed, rel)
		}
	}
	sort.Strings(p.Removed)
	p.NoChange = len(p.Upload) == 0 && len(p.Removed) == 0

	dropping := map[string]bool{}
	for _, g := range pointer.Dropping {
		dropping[g] = true
	}
	seen := map[string]bool{}
	var existing []string
	for _, g := range pointer.Generations {
		if g != "" && !dropping[g] && !seen[g] {
			seen[g] = true
			existing = append(existing, g)
		}
	}
	if p.NoChange {
		p.Kept = existing
		return p
	}
	if keep < 1 {
		keep = 1
	}
	all := append([]string{gen}, existing...)
	if len(all) > keep {
		p.Kept, p.Drop = all[:keep], all[keep:]
	} else {
		p.Kept = all
	}
	return p
}
```

- [ ] **Step 4: Run the plan test**

Run: `go test ./internal/backup/ -run TestMakePlan -v`
Expected: PASS

- [ ] **Step 5: Write the failing Run tests**

```go
// internal/backup/run_test.go
package backup

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"lossless/internal/backup/s3"
	"lossless/internal/backup/s3/s3test"
)

// setupBackup seeds a store, points backup.env at the fake bucket, and
// silences retry sleeps.
func setupBackup(t *testing.T, srv *s3test.Server, keep int) string {
	t.Helper()
	clearBackupEnv(t)
	t.Setenv("LOSSLESS_BACKUP_ACCESS_KEY", "test")
	t.Setenv("LOSSLESS_BACKUP_SECRET_KEY", "secret")
	t.Setenv("LOSSLESS_CLIENT", "")
	home, _ := seedHome(t)
	must(t, Init(home, InitOptions{URL: "s3://bkt/pre", Endpoint: srv.URL(), Every: time.Hour, Keep: keep}))
	sleepHook = func(time.Duration) {}
	t.Cleanup(func() { sleepHook = nil })
	return home
}

func runOK(t *testing.T, home string, o RunOptions) Summary {
	t.Helper()
	sum, err := Run(context.Background(), home, o)
	if err != nil {
		t.Fatal(err)
	}
	return sum
}

func pointerOf(t *testing.T, home string, srv *s3test.Server) *Manifest {
	t.Helper()
	keys, err := LoadKey(home)
	must(t, err)
	r := &remote{keys: keys}
	c, err := s3.New(srv.Config("bkt", "pre"))
	must(t, err)
	r.c = c
	m, err := r.getManifest(context.Background(), pointerKey)
	must(t, err)
	return m
}

func touchLive(t *testing.T, home, line string) {
	t.Helper()
	p := filepath.Join(home, "raw", "acme__api", "2026-09", "live.jsonl")
	f, err := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0o600)
	must(t, err)
	_, _ = f.WriteString(line + "\n")
	must(t, f.Close())
	now := time.Now().Add(2 * time.Second)
	must(t, os.Chtimes(p, now, now))
}

func TestRunFirstThenNoChange(t *testing.T) {
	srv := s3test.New()
	defer srv.Close()
	home := setupBackup(t, srv, 5)
	sum := runOK(t, home, RunOptions{})
	if sum.Uploaded != sum.Scanned || sum.Uploaded < 5 || sum.Generation == "" || sum.NoChange {
		t.Fatalf("%+v", sum)
	}
	m := pointerOf(t, home, srv)
	if m.Generation != sum.Generation || len(m.Generations) != 1 || len(m.Files) != sum.Uploaded {
		t.Fatalf("%+v", m)
	}
	if _, ok := srv.Object("bkt", "pre/"+manifestKey(sum.Generation)); !ok {
		t.Fatal("m/<generation> missing")
	}
	puts := srv.Count("PUT")
	again := runOK(t, home, RunOptions{})
	if !again.NoChange || srv.Count("PUT") != puts {
		t.Fatalf("second run must write nothing: %+v puts=%d", again, srv.Count("PUT"))
	}
	st := LoadState(home)
	if st.LastOK == "" || st.LastError != "" || st.LastGeneration != sum.Generation {
		t.Fatalf("state %+v", st)
	}
}

func TestRunLiveGrowthMakesGenerationKeepsOld(t *testing.T) {
	srv := s3test.New()
	defer srv.Close()
	home := setupBackup(t, srv, 5)
	first := runOK(t, home, RunOptions{})
	oldLive := pointerOf(t, home, srv).Files["raw/acme__api/2026-09/live.jsonl"].Object
	touchLive(t, home, `{"role":"assistant","content":"more"}`)
	second := runOK(t, home, RunOptions{})
	if second.Uploaded != 1 || second.Deleted != 0 || second.Generation == first.Generation {
		t.Fatalf("%+v", second)
	}
	m := pointerOf(t, home, srv)
	if strings.Join(m.Generations, ",") != second.Generation+","+first.Generation {
		t.Fatal(m.Generations)
	}
	if _, ok := srv.Object("bkt", "pre/"+oldLive); !ok {
		t.Fatal("old live version must survive while its generation is kept")
	}
}

func TestRunPrunedClaimLeavesManifest(t *testing.T) {
	srv := s3test.New()
	defer srv.Close()
	home := setupBackup(t, srv, 5)
	runOK(t, home, RunOptions{})
	mds, _ := filepath.Glob(filepath.Join(home, "export", "acme__api", "*.md"))
	must(t, os.Remove(mds[0]))
	sum := runOK(t, home, RunOptions{})
	if sum.NoChange || sum.Uploaded != 0 {
		t.Fatalf("%+v", sum)
	}
	if _, ok := pointerOf(t, home, srv).Files["export/acme__api/"+filepath.Base(mds[0])]; ok {
		t.Fatal("pruned claim still in pointer")
	}
}

func TestGenerationsDropBeyondKeep(t *testing.T) {
	srv := s3test.New()
	defer srv.Close()
	home := setupBackup(t, srv, 2)
	g1 := runOK(t, home, RunOptions{}).Generation
	live1 := pointerOf(t, home, srv).Files["raw/acme__api/2026-09/live.jsonl"].Object
	sealed := pointerOf(t, home, srv).Files["raw/acme__api/2026-09/sealed.jsonl.zst"].Object
	touchLive(t, home, "2")
	g2 := runOK(t, home, RunOptions{}).Generation
	touchLive(t, home, "3")
	third := runOK(t, home, RunOptions{})
	if strings.Join(third.Dropped, ",") != g1 || third.Deleted != 1 {
		t.Fatalf("%+v", third)
	}
	m := pointerOf(t, home, srv)
	if strings.Join(m.Generations, ",") != third.Generation+","+g2 || len(m.Dropping) != 0 {
		t.Fatalf("%+v", m)
	}
	if _, ok := srv.Object("bkt", "pre/"+live1); ok {
		t.Fatal("generation 1's unique live part must be deleted")
	}
	if _, ok := srv.Object("bkt", "pre/"+sealed); !ok {
		t.Fatal("shared sealed part must stay")
	}
	if _, ok := srv.Object("bkt", "pre/"+manifestKey(g1)); ok {
		t.Fatal("m/g1 must be deleted")
	}
}

func TestDroppingFinishesOnNextRun(t *testing.T) {
	srv := s3test.New()
	defer srv.Close()
	home := setupBackup(t, srv, 1)
	g1 := runOK(t, home, RunOptions{}).Generation
	live1 := pointerOf(t, home, srv).Files["raw/acme__api/2026-09/live.jsonl"].Object
	srv.FailNext("bkt", "pre/"+live1, "500", 4) // one full retry cycle
	touchLive(t, home, "2")
	second := runOK(t, home, RunOptions{})
	m := pointerOf(t, home, srv)
	if strings.Join(m.Dropping, ",") != g1 || len(second.Dropped) != 0 {
		t.Fatalf("pointer must record the pending drop: %+v %+v", m, second)
	}
	if _, ok := srv.Object("bkt", "pre/"+manifestKey(g1)); !ok {
		t.Fatal("m/g1 must survive a failed drop")
	}
	third := runOK(t, home, RunOptions{}) // no change, but finishes the drop
	if !third.NoChange || strings.Join(third.Dropped, ",") != g1 {
		t.Fatalf("%+v", third)
	}
	if _, ok := srv.Object("bkt", "pre/"+manifestKey(g1)); ok {
		t.Fatal("m/g1 must be gone")
	}
	touchLive(t, home, "3")
	runOK(t, home, RunOptions{})
	if m := pointerOf(t, home, srv); containsString(m.Dropping, g1) {
		t.Fatalf("a finished drop must not be re-listed: %v", m.Dropping)
	}
}

func TestCommitPointFailedPointerKeepsPrevious(t *testing.T) {
	srv := s3test.New()
	defer srv.Close()
	home := setupBackup(t, srv, 5)
	g1 := runOK(t, home, RunOptions{}).Generation
	touchLive(t, home, "2")
	srv.FailNext("bkt", "pre/manifest", "503", 4) // one full retry cycle
	if _, err := Run(context.Background(), home, RunOptions{}); err == nil {
		t.Fatal("pointer PUT failure must fail the run")
	}
	if m := pointerOf(t, home, srv); m.Generation != g1 {
		t.Fatal("previous pointer must be intact")
	}
	if st := LoadState(home); st.LastError == "" {
		t.Fatal("last_error must be stamped")
	}
	sum := runOK(t, home, RunOptions{})
	if sum.Uploaded != 1 {
		t.Fatalf("orphan object must be re-PUT so it becomes referenced: %+v", sum)
	}
}

func TestWriterGuardAndTakeOver(t *testing.T) {
	srv := s3test.New()
	defer srv.Close()
	homeA := setupBackup(t, srv, 5)
	runOK(t, homeA, RunOptions{})
	homeB, _ := seedHome(t)
	must(t, copyFile(filepath.Join(homeA, "backup.env"), filepath.Join(homeB, "backup.env")))
	must(t, copyFile(filepath.Join(homeA, "backup.key"), filepath.Join(homeB, "backup.key")))
	puts := srv.Count("PUT")
	var ow *OtherWriterError
	if _, err := Run(context.Background(), homeB, RunOptions{}); !errors.As(err, &ow) {
		t.Fatalf("want OtherWriterError, got %v", err)
	}
	if srv.Count("PUT") != puts {
		t.Fatal("guard must refuse before uploading")
	}
	runOK(t, homeB, RunOptions{TakeOver: true})
	if _, err := Run(context.Background(), homeA, RunOptions{}); !errors.As(err, &ow) {
		t.Fatalf("A must now refuse, got %v", err)
	}
}


func TestDryRunWritesNothing(t *testing.T) {
	srv := s3test.New()
	defer srv.Close()
	home := setupBackup(t, srv, 5)
	sum := runOK(t, home, RunOptions{DryRun: true})
	if !sum.DryRun || srv.Count("PUT") != 0 {
		t.Fatalf("%+v puts=%d", sum, srv.Count("PUT"))
	}
	if _, err := os.Stat(filepath.Join(home, "backup-state.json")); !os.IsNotExist(err) {
		t.Fatal("dry run must not write state")
	}
}

func TestRunRefusesWhenLocked(t *testing.T) {
	srv := s3test.New()
	defer srv.Close()
	home := setupBackup(t, srv, 5)
	release, err := Lock(home)
	must(t, err)
	defer release()
	if _, err := Run(context.Background(), home, RunOptions{}); !errors.Is(err, ErrLocked) {
		t.Fatalf("want ErrLocked, got %v", err)
	}
}

func TestPointerRateLimitIsRetried(t *testing.T) {
	srv := s3test.New()
	defer srv.Close()
	srv.RateLimit = true
	home := setupBackup(t, srv, 5)
	sleepHook = time.Sleep // the 1s backoff is what clears R2's per-key limit
	runOK(t, home, RunOptions{})
	touchLive(t, home, "2")
	runOK(t, home, RunOptions{})
}
```


- [ ] **Step 6: Run them to verify they fail**

Run: `go test ./internal/backup/ -run 'TestRun|TestGenerations|TestDropping|TestCommit|TestWriter|TestDryRun|TestPointer' -v`
Expected: FAIL — undefined: Run, RunOptions, sleepHook

- [ ] **Step 7: Write run.go**

```go
// internal/backup/run.go
package backup

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"lossless/internal/backup/s3"
	"lossless/internal/version"
	"lossless/internal/write"
)

const parallel = 4

// sleepHook replaces the retry sleeper when set. Tests use it.
var sleepHook func(time.Duration)

type RunOptions struct {
	DryRun   bool
	Verbose  bool
	TakeOver bool
	Out      io.Writer
	Now      func() time.Time
}

type Summary struct {
	Scanned    int
	Uploaded   int
	Deleted    int
	Bytes      int64
	Generation string
	NoChange   bool
	DryRun     bool
	Dropped    []string
	Elapsed    time.Duration
}

// OtherWriterError: the pointer was last written by an install this one
// has not adopted.
type OtherWriterError struct {
	Client    string
	CreatedAt string
}

func (e *OtherWriterError) Error() string {
	return fmt.Sprintf("bucket last written by %s at %s; run lossless backup --take-over if that machine is retired", e.Client, e.CreatedAt)
}

func newRemote(cfg *Config, home string) (*remote, error) {
	keys, err := LoadKey(home)
	if err != nil {
		return nil, err
	}
	c, err := s3.New(cfg.S3)
	if err != nil {
		return nil, err
	}
	if sleepHook != nil {
		c.SetSleep(sleepHook)
	}
	return &remote{c: c, keys: keys}, nil
}

// Run performs one backup. It is single-flight per home. State is saved
// on success and on failure (to stamp last_error), never on a dry run.
func Run(ctx context.Context, home string, o RunOptions) (Summary, error) {
	if o.Out == nil {
		o.Out = io.Discard
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	start := o.Now().UTC()
	var sum Summary
	cfg, err := LoadConfig(home)
	if err != nil {
		return sum, &ConfigError{Err: err}
	}
	rm, err := newRemote(cfg, home)
	if err != nil {
		return sum, &ConfigError{Err: err}
	}
	release, err := Lock(home)
	if err != nil {
		return sum, err
	}
	defer release()
	state := LoadState(home)
	state.LastAttempt = start.Format(time.RFC3339)
	err = run(ctx, home, cfg, rm, state, o, start, &sum)
	if !o.DryRun {
		if err != nil {
			state.LastError = err.Error()
		} else {
			state.LastOK = o.Now().UTC().Format(time.RFC3339)
			state.LastError = ""
		}
		if serr := state.Save(home); serr != nil && err == nil {
			err = serr
		}
	}
	sum.Elapsed = o.Now().Sub(start)
	return sum, err
}

func run(ctx context.Context, home string, cfg *Config, rm *remote, state *State, o RunOptions, start time.Time, sum *Summary) error {
	me := write.ClientID(home)
	pointer, err := rm.getManifest(ctx, pointerKey)
	if errors.Is(err, s3.ErrNotFound) {
		pointer = emptyManifest()
	} else if err != nil {
		return err
	}
	if o.TakeOver {
		state.Adopt(pointer.Client)
	}
	if !state.AllowsWriter(me, pointer.Client) {
		return &OtherWriterError{Client: pointer.Client, CreatedAt: pointer.CreatedAt}
	}

	tmp := tmpDir(home)
	_ = os.RemoveAll(tmp)
	if err := os.MkdirAll(tmp, 0o700); err != nil {
		return err
	}
	defer os.RemoveAll(tmp)

	items, err := walk(home, tmp, rm.keys, state)
	if err != nil {
		return err
	}
	sum.Scanned = len(items)
	gen := NewGeneration(start)
	p := makePlan(items, pointer, cfg.Keep, gen)

	if o.DryRun {
		sum.DryRun = true
		printPlan(o.Out, p, gen, pointer.Dropping)
		return nil
	}

	// Finish drops a previous run left pending before anything else.
	kept := p.Kept
	if !p.NoChange {
		kept = p.Kept[1:] // existing generations; the new one has no manifest yet
	}
	pending := dropGenerations(ctx, rm, state, kept, nil, pointer.Dropping, o.Out, sum)

	current := pointer
	if p.NoChange {
		sum.NoChange = true
		fmt.Fprintln(o.Out, "no change")
	} else {
		if err := uploadAll(ctx, rm, tmp, items, p.Upload, o, sum); err != nil {
			return err
		}
		current = &Manifest{
			Version:     manifestVersion,
			Generation:  gen,
			CreatedAt:   start.Format(time.RFC3339),
			Lossless:    version.Version,
			Client:      me,
			Generations: p.Kept,
			Dropping:    append(append([]string{}, pending...), p.Drop...),
			Files:       filesFrom(items),
		}
		if err := rm.putManifest(ctx, manifestKey(gen), current); err != nil {
			return err
		}
		if err := rm.putManifest(ctx, pointerKey, current); err != nil {
			return err
		}
		state.Manifests[gen] = current
		state.LastGeneration = gen
		sum.Generation = gen
		dropGenerations(ctx, rm, state, p.Kept, current, p.Drop, o.Out, sum)
	}

	state.Files = make(map[string]FileState, len(items))
	for _, it := range items {
		state.Files[it.Rel] = FileState{Size: it.Size, StatSize: it.StatSize, Mtime: it.Mtime, SHA256: it.SHA256, Object: it.Object}
	}
	for g := range state.Manifests {
		if !containsString(current.Generations, g) {
			delete(state.Manifests, g)
		}
	}
	return nil
}

func containsString(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func printPlan(out io.Writer, p plan, gen string, pending []string) {
	fmt.Fprintf(out, "dry run: generation %s\n", gen)
	if p.NoChange {
		fmt.Fprintln(out, "no change")
	}
	for _, it := range p.Upload {
		fmt.Fprintf(out, "upload  %s (%d bytes)\n", it.Rel, it.Size)
	}
	for _, rel := range p.Removed {
		fmt.Fprintf(out, "remove  %s\n", rel)
	}
	for _, g := range append(append([]string{}, pending...), p.Drop...) {
		fmt.Fprintf(out, "drop    %s\n", g)
	}
}

// uploadAll encrypts and uploads up to four items at a time. If a file's
// content no longer matches its cached hash (changed without a new mtime),
// the item is re-keyed from the real plaintext hash so the manifest and the
// object name agree.
func uploadAll(ctx context.Context, rm *remote, tmp string, all []Item, todo []Item, o RunOptions, sum *Summary) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	index := map[string]int{}
	for i, it := range all {
		index[it.Rel] = i
	}
	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		firstErr error
	)
	sem := make(chan struct{}, parallel)
	for _, it := range todo {
		if ctx.Err() != nil {
			break
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(it Item) {
			defer wg.Done()
			defer func() { <-sem }()
			fixed, n, err := uploadOne(ctx, rm, tmp, it)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				if firstErr == nil {
					firstErr = err
					cancel()
				}
				return
			}
			all[index[it.Rel]] = fixed
			sum.Uploaded++
			sum.Bytes += n
			if o.Verbose {
				fmt.Fprintf(o.Out, "upload  %s (%d bytes)\n", it.Rel, fixed.Size)
			}
		}(it)
	}
	wg.Wait()
	return firstErr
}

type countReader struct {
	r io.Reader
	n int64
}

func (c *countReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	return n, err
}

func uploadOne(ctx context.Context, rm *remote, tmp string, it Item) (Item, int64, error) {
	src, err := os.Open(it.Src)
	if err != nil {
		return it, 0, err
	}
	defer src.Close()
	enc := filepath.Join(tmp, rm.keys.Name(it.Rel)+"."+it.SHA256[:16]+".enc")
	out, err := os.OpenFile(enc, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return it, 0, err
	}
	defer os.Remove(enc)
	cr := &countReader{r: src}
	plainSHA, cipherSHA, n, err := rm.keys.Encrypt(out, cr, it.Rel)
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return it, 0, err
	}
	if plainSHA != it.SHA256 {
		it.SHA256 = plainSHA
		it.Size = cr.n
		it.Object = objectKey(rm.keys.Name(it.Rel), plainSHA)
	}
	f, err := os.Open(enc)
	if err != nil {
		return it, 0, err
	}
	defer f.Close()
	if err := rm.c.Put(ctx, it.Object, f, n, cipherSHA); err != nil {
		return it, 0, err
	}
	return it, n, nil
}

// dropGenerations deletes the objects that only the dropped generations
// reference, then their manifests. kept lists the generations whose
// objects must survive; current (may be nil) is the just-written manifest
// that is not yet in the state cache. It returns the generations whose
// removal did not finish; the pointer keeps listing those under Dropping.
func dropGenerations(ctx context.Context, rm *remote, state *State, kept []string, current *Manifest, drop []string, out io.Writer, sum *Summary) (remaining []string) {
	if len(drop) == 0 {
		return nil
	}
	refs := map[string]bool{}
	for _, g := range kept {
		m := state.Manifests[g]
		if m == nil && current != nil && g == current.Generation {
			m = current
		}
		if m == nil {
			fetched, err := rm.getManifest(ctx, manifestKey(g))
			if err != nil {
				fmt.Fprintf(out, "backup: cannot read kept generation %s (%v); leaving drops pending\n", g, err)
				return drop
			}
			m = fetched
			state.Manifests[g] = m
		}
		for _, e := range m.Files {
			refs[e.Object] = true
		}
	}
	for _, g := range drop {
		m := state.Manifests[g]
		if m == nil {
			fetched, err := rm.getManifest(ctx, manifestKey(g))
			if errors.Is(err, s3.ErrNotFound) {
				delete(state.Manifests, g)
				sum.Dropped = append(sum.Dropped, g)
				continue // already gone: done
			}
			if err != nil {
				fmt.Fprintf(out, "backup: drop %s: %v\n", g, err)
				remaining = append(remaining, g)
				continue
			}
			m = fetched
		}
		failed := false
		for _, e := range m.Files {
			if refs[e.Object] {
				continue
			}
			if err := rm.c.Delete(ctx, e.Object); err != nil {
				failed = true
				fmt.Fprintf(out, "backup: delete %s: %v\n", e.Object, err)
				continue
			}
			sum.Deleted++
		}
		if failed {
			remaining = append(remaining, g)
			continue
		}
		if err := rm.c.Delete(ctx, manifestKey(g)); err != nil {
			fmt.Fprintf(out, "backup: delete %s: %v\n", manifestKey(g), err)
			remaining = append(remaining, g)
			continue
		}
		delete(state.Manifests, g)
		sum.Dropped = append(sum.Dropped, g)
	}
	return remaining
}
```

Note on the spec: the spec says object deletes run four in flight. A dropped generation owns a handful of unique objects (its live parts and index snapshots), so this task deletes sequentially. Task 10 updates the spec's step 7 to say so.

- [ ] **Step 8: Run the backup tests**

Run: `go test ./internal/backup/... -v`
Expected: PASS. `TestPointerRateLimitIsRetried` takes about one second because it uses the real backoff once.

- [ ] **Step 9: gofmt, vet, commit**

```bash
gofmt -l internal && go vet ./... && \
git add internal/backup && \
git commit -m "Add backup plan and Run: uploads, generations, writer guard."
```

---

### Task 7: Restore and List

**Files:**
- Create: `internal/backup/restore.go`
- Test: `internal/backup/restore_test.go`

**Interfaces:**
- Consumes: `remote`, `getManifest`, `hashFile` (Task 5); `LoadConfig`, `LoadKey`, `Lock`, `LoadState`, `State.Adopt`, `Manifest`, `manifestKey`, `pointerKey` (Task 4); `crypt.Keys.Decrypt` (Task 3); `s3.ErrNotFound` (Task 2); `env.Sidecar()` (existing: `""` when `LOSSLESS_SIDECAR` is off).
- Produces:
  - `type RestoreOptions struct { Force bool; At string; Out io.Writer; Health func(url string) bool }` — `Health` nil means a real GET of `<sidecar>/health`
  - `type RestoreSummary struct { Generation string; Restored, Skipped, LeftAlone int; Bytes int64; Left []string }`
  - `func Restore(ctx context.Context, home string, o RestoreOptions) (RestoreSummary, error)`
  - `type GenerationInfo struct { Generation, CreatedAt, Lossless, Client string; Files int; Bytes int64; Latest bool }`
  - `func List(ctx context.Context, home string) ([]GenerationInfo, error)`
  - `var ErrDaemonRunning`, `var ErrStoreNotEmpty`, `var ErrNothingToRestore`

- [ ] **Step 1: Write the failing restore tests**

```go
// internal/backup/restore_test.go
package backup

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"lossless/internal/backup/s3/s3test"
	"lossless/internal/store"
)

func noDaemon(string) bool { return false }

// freshTarget imitates a new machine after `lossless setup`: an empty store
// whose index exists, plus the copied backup.env and backup.key.
func freshTarget(t *testing.T, from string) string {
	t.Helper()
	home := t.TempDir()
	st, err := store.Open(home)
	must(t, err)
	must(t, st.Close())
	must(t, copyFile(filepath.Join(from, "backup.env"), filepath.Join(home, "backup.env")))
	must(t, copyFile(filepath.Join(from, "backup.key"), filepath.Join(home, "backup.key")))
	return home
}

func claimTexts(t *testing.T, home string) []string {
	t.Helper()
	st, err := store.Open(home)
	must(t, err)
	defer st.Close()
	active, err := st.ListActive("acme/api")
	must(t, err)
	var out []string
	for _, c := range active {
		out = append(out, c.Text)
	}
	sort.Strings(out)
	return out
}

func TestRestoreIntoFreshStoreThenBackupJustWorks(t *testing.T) {
	srv := s3test.New()
	defer srv.Close()
	src := setupBackup(t, srv, 5)
	first := runOK(t, src, RunOptions{})
	dst := freshTarget(t, src)
	sum, err := Restore(context.Background(), dst, RestoreOptions{Health: noDaemon})
	if err != nil {
		t.Fatal(err)
	}
	if sum.Generation != first.Generation || sum.Restored != first.Uploaded || sum.LeftAlone != 0 {
		t.Fatalf("%+v", sum)
	}
	if strings.Join(claimTexts(t, dst), "|") != strings.Join(claimTexts(t, src), "|") {
		t.Fatal("claims differ after restore")
	}
	a, _ := os.ReadFile(filepath.Join(src, "raw", "acme__api", "2026-09", "sealed.jsonl.zst"))
	b, _ := os.ReadFile(filepath.Join(dst, "raw", "acme__api", "2026-09", "sealed.jsonl.zst"))
	if string(a) != string(b) {
		t.Fatal("sealed part differs")
	}
	for _, rel := range []string{"raw/acme__api/2026-09/live.jsonl", "index/claims.sqlite"} {
		st, err := os.Stat(filepath.Join(dst, filepath.FromSlash(rel)))
		if err != nil || st.Mode().Perm() != 0o600 {
			t.Fatalf("%s: %v %v", rel, err, st)
		}
	}
	if _, err := os.Stat(filepath.Join(dst, "index", "claims.sqlite-wal")); !os.IsNotExist(err) {
		t.Fatal("a stale WAL next to the restored index must be removed")
	}
	state := LoadState(dst)
	if state.LastOK != "" || len(state.Files) != first.Uploaded || len(state.Adopted) != 1 {
		t.Fatalf("state %+v", state)
	}
	// The new machine is now the writer without --take-over.
	again := runOK(t, dst, RunOptions{})
	if !again.NoChange {
		t.Fatalf("restored store must be in sync: %+v", again)
	}
	// Restoring again skips everything.
	sum2, err := Restore(context.Background(), dst, RestoreOptions{Health: noDaemon, Force: true})
	if err != nil || sum2.Restored != 0 || sum2.Skipped != first.Uploaded {
		t.Fatalf("%+v %v", sum2, err)
	}
}

func TestRestoreGuards(t *testing.T) {
	srv := s3test.New()
	defer srv.Close()
	src := setupBackup(t, srv, 5)
	runOK(t, src, RunOptions{})
	dst := freshTarget(t, src)
	if _, err := Restore(context.Background(), dst, RestoreOptions{Health: func(string) bool { return true }}); !errors.Is(err, ErrDaemonRunning) {
		t.Fatalf("want ErrDaemonRunning, got %v", err)
	}
	// A seeded store is not empty.
	busy, _ := seedHome(t)
	must(t, copyFile(filepath.Join(src, "backup.env"), filepath.Join(busy, "backup.env")))
	must(t, copyFile(filepath.Join(src, "backup.key"), filepath.Join(busy, "backup.key")))
	if _, err := Restore(context.Background(), busy, RestoreOptions{Health: noDaemon}); !errors.Is(err, ErrStoreNotEmpty) {
		t.Fatalf("want ErrStoreNotEmpty, got %v", err)
	}
	touchLive(t, busy, `{"role":"user","content":"local only"}`) // now differs from the bucket
	sum, err := Restore(context.Background(), busy, RestoreOptions{Health: noDaemon, Force: true})
	if err != nil {
		t.Fatal(err)
	}
	// busy has its own claim files (different ids) and its own live part:
	// those are left alone; the index snapshots are replaced.
	if sum.LeftAlone != 1 || strings.Join(sum.Left, ",") != "raw/acme__api/2026-09/live.jsonl" {
		t.Fatalf("%+v", sum)
	}
	for _, l := range sum.Left {
		if strings.HasPrefix(l, "index/") {
			t.Fatalf("index must be overwritten under --force: %v", sum.Left)
		}
	}
	// Sidecar off: warning, no refusal.
	t.Setenv("LOSSLESS_SIDECAR", "off")
	var out strings.Builder
	fresh := freshTarget(t, src)
	if _, err := Restore(context.Background(), fresh, RestoreOptions{Out: &out}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "stop lossless serve") {
		t.Fatalf("expected a warning, got %q", out.String())
	}
}

func TestRestoreAtAndList(t *testing.T) {
	srv := s3test.New()
	defer srv.Close()
	src := setupBackup(t, srv, 5)
	g1 := runOK(t, src, RunOptions{}).Generation
	touchLive(t, src, `{"role":"assistant","content":"second"}`)
	g2 := runOK(t, src, RunOptions{}).Generation
	gens, err := List(context.Background(), src)
	if err != nil || len(gens) != 2 || gens[0].Generation != g2 || !gens[0].Latest || gens[1].Generation != g1 || gens[1].Latest {
		t.Fatalf("%+v %v", gens, err)
	}
	if gens[0].Files == 0 || gens[0].Bytes == 0 || gens[0].Lossless == "" {
		t.Fatalf("%+v", gens[0])
	}
	dst := freshTarget(t, src)
	sum, err := Restore(context.Background(), dst, RestoreOptions{Health: noDaemon, At: g1})
	if err != nil || sum.Generation != g1 {
		t.Fatalf("%+v %v", sum, err)
	}
	b, _ := os.ReadFile(filepath.Join(dst, "raw", "acme__api", "2026-09", "live.jsonl"))
	if strings.Contains(string(b), "second") {
		t.Fatal("--at g1 must restore the older live part")
	}
	if _, err := Restore(context.Background(), freshTarget(t, src), RestoreOptions{Health: noDaemon, At: "nope"}); err == nil || !strings.Contains(err.Error(), g2) {
		t.Fatalf("unknown generation must list the kept ones: %v", err)
	}
}

func TestRestoreNothingToRestore(t *testing.T) {
	srv := s3test.New()
	defer srv.Close()
	src := setupBackup(t, srv, 5)
	if _, err := Restore(context.Background(), freshTarget(t, src), RestoreOptions{Health: noDaemon}); !errors.Is(err, ErrNothingToRestore) {
		t.Fatalf("want ErrNothingToRestore, got %v", err)
	}
}
```


- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./internal/backup/ -run 'TestRestore' -v`
Expected: FAIL — undefined: Restore, RestoreOptions, List

- [ ] **Step 3: Write restore.go**

```go
// internal/backup/restore.go
package backup

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"lossless/internal/backup/s3"
	"lossless/internal/env"
)

var (
	ErrDaemonRunning    = errors.New("lossless serve is running; stop it before restore")
	ErrStoreNotEmpty    = errors.New("store already has raw or export files; use --force to union")
	ErrNothingToRestore = errors.New("nothing to restore: the bucket has no manifest")
)

type RestoreOptions struct {
	Force  bool
	At     string
	Out    io.Writer
	Health func(url string) bool // nil = real probe
}

type RestoreSummary struct {
	Generation string
	Restored   int
	Skipped    int
	LeftAlone  int
	Bytes      int64
	Left       []string
}

func probeHealth(url string) bool {
	c := &http.Client{Timeout: 2 * time.Second}
	res, err := c.Get(strings.TrimRight(url, "/") + "/health")
	if err != nil {
		return false
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 4096))
	_ = res.Body.Close()
	return res.StatusCode == 200
}

func hasRegularFile(dir string) bool {
	found := false
	_ = filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.Type().IsRegular() {
			found = true
			return fs.SkipAll
		}
		return nil
	})
	return found
}

// Restore pulls one generation (latest, or o.At) into home.
func Restore(ctx context.Context, home string, o RestoreOptions) (RestoreSummary, error) {
	var sum RestoreSummary
	if o.Out == nil {
		o.Out = io.Discard
	}
	if o.Health == nil {
		o.Health = probeHealth
	}
	if url := env.Sidecar(); url == "" {
		fmt.Fprintln(o.Out, "warning: LOSSLESS_SIDECAR is off, cannot check for a running daemon; stop lossless serve before restoring")
	} else if o.Health(url) {
		return sum, ErrDaemonRunning
	}
	if !o.Force && (hasRegularFile(filepath.Join(home, "raw")) || hasRegularFile(filepath.Join(home, "export"))) {
		return sum, ErrStoreNotEmpty
	}
	cfg, err := LoadConfig(home)
	if err != nil {
		return sum, &ConfigError{Err: err}
	}
	rm, err := newRemote(cfg, home)
	if err != nil {
		return sum, &ConfigError{Err: err}
	}
	release, err := Lock(home)
	if err != nil {
		return sum, err
	}
	defer release()

	pointer, err := rm.getManifest(ctx, pointerKey)
	if errors.Is(err, s3.ErrNotFound) {
		return sum, ErrNothingToRestore
	}
	if err != nil {
		return sum, err
	}
	m := pointer
	if o.At != "" && o.At != pointer.Generation {
		if containsString(pointer.Dropping, o.At) || !containsString(pointer.Generations, o.At) {
			return sum, fmt.Errorf("generation %s is not kept; kept: %s", o.At, strings.Join(pointer.Generations, ", "))
		}
		if m, err = rm.getManifest(ctx, manifestKey(o.At)); err != nil {
			return sum, err
		}
	}
	sum.Generation = m.Generation

	rels := make([]string, 0, len(m.Files))
	for rel := range m.Files {
		rels = append(rels, rel)
	}
	sort.Strings(rels)

	state := &State{Files: map[string]FileState{}, Manifests: map[string]*Manifest{}}
	var (
		mu       sync.Mutex
		wg       sync.WaitGroup
		firstErr error
	)
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	sem := make(chan struct{}, parallel)
	for _, rel := range rels {
		if ctx.Err() != nil {
			break
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(rel string, e FileEntry) {
			defer wg.Done()
			defer func() { <-sem }()
			outcome, n, err := restoreOne(ctx, rm, home, rel, e)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				if firstErr == nil {
					firstErr = fmt.Errorf("%s: %w", rel, err)
					cancel()
				}
				return
			}
			switch outcome {
			case "restored":
				sum.Restored++
				sum.Bytes += n
			case "skipped":
				sum.Skipped++
			case "left":
				sum.LeftAlone++
				sum.Left = append(sum.Left, rel)
				return
			}
			if st, err := os.Stat(filepath.Join(home, filepath.FromSlash(rel))); err == nil {
				state.Files[rel] = FileState{Size: e.Size, StatSize: st.Size(), Mtime: st.ModTime().UnixNano(), SHA256: e.SHA256, Object: e.Object}
			}
		}(rel, m.Files[rel])
	}
	wg.Wait()
	if firstErr != nil {
		return sum, firstErr
	}
	sort.Strings(sum.Left)
	state.Adopt(pointer.Client)
	state.Manifests[m.Generation] = m
	if err := state.Save(home); err != nil {
		return sum, err
	}
	return sum, nil
}

// restoreOne returns "restored", "skipped" (already at this hash), or
// "left" (a differing raw/export file that is never overwritten).
func restoreOne(ctx context.Context, rm *remote, home, rel string, e FileEntry) (string, int64, error) {
	target := filepath.Join(home, filepath.FromSlash(rel))
	if st, err := os.Stat(target); err == nil && st.Mode().IsRegular() {
		if sha, _, err := hashFile(target); err == nil && sha == e.SHA256 {
			return "skipped", 0, nil
		}
		if !strings.HasPrefix(rel, "index/") {
			return "left", 0, nil
		}
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		return "", 0, err
	}
	body, _, err := rm.c.Get(ctx, e.Object)
	if err != nil {
		return "", 0, err
	}
	defer body.Close()
	tmp := target + ".restore-tmp"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return "", 0, err
	}
	sha, derr := rm.keys.Decrypt(out, body, rel)
	if cerr := out.Close(); derr == nil {
		derr = cerr
	}
	if derr != nil {
		_ = os.Remove(tmp)
		return "", 0, derr
	}
	if sha != e.SHA256 {
		_ = os.Remove(tmp)
		return "", 0, fmt.Errorf("hash mismatch after decrypt")
	}
	if strings.HasSuffix(rel, ".sqlite") {
		// A leftover WAL from the empty store would corrupt the snapshot.
		_ = os.Remove(target + "-wal")
		_ = os.Remove(target + "-shm")
	}
	if err := os.Rename(tmp, target); err != nil {
		_ = os.Remove(tmp)
		return "", 0, err
	}
	return "restored", e.Size, nil
}

type GenerationInfo struct {
	Generation string
	CreatedAt  string
	Lossless   string
	Client     string
	Files      int
	Bytes      int64
	Latest     bool
}

// List describes the kept generations, newest first.
func List(ctx context.Context, home string) ([]GenerationInfo, error) {
	cfg, err := LoadConfig(home)
	if err != nil {
		return nil, &ConfigError{Err: err}
	}
	rm, err := newRemote(cfg, home)
	if err != nil {
		return nil, &ConfigError{Err: err}
	}
	pointer, err := rm.getManifest(ctx, pointerKey)
	if errors.Is(err, s3.ErrNotFound) {
		return nil, ErrNothingToRestore
	}
	if err != nil {
		return nil, err
	}
	var out []GenerationInfo
	for _, g := range pointer.Generations {
		if containsString(pointer.Dropping, g) {
			continue
		}
		m := pointer
		if g != pointer.Generation {
			m, err = rm.getManifest(ctx, manifestKey(g))
			if errors.Is(err, s3.ErrNotFound) {
				continue
			}
			if err != nil {
				return nil, err
			}
		}
		info := GenerationInfo{Generation: m.Generation, CreatedAt: m.CreatedAt, Lossless: m.Lossless, Client: m.Client, Files: len(m.Files), Latest: g == pointer.Generation}
		for _, e := range m.Files {
			info.Bytes += e.Size
		}
		out = append(out, info)
	}
	return out, nil
}
```

- [ ] **Step 4: Run the backup tests**

Run: `go test ./internal/backup/... -v`
Expected: PASS

- [ ] **Step 5: gofmt, vet, commit**

```bash
gofmt -l internal && go vet ./... && \
git add internal/backup && \
git commit -m "Add backup Restore and List."
```

---

### Task 8: Scheduler, doctor check, serve wiring

**Files:**
- Create: `internal/backup/schedule.go`
- Create: `internal/backup/doctor.go`
- Modify: `internal/serve/serve.go:276-281` (start the scheduler next to the watcher)
- Test: `internal/backup/schedule_test.go`
- Test: `internal/backup/doctor_test.go`

**Interfaces:**
- Consumes: `LoadConfig`, `LoadState`, `State.LastOKTime`, `ErrNotConfigured`, `ErrLocked` (Task 4/6); `Run`, `RunOptions`, `Summary` (Task 6).
- Produces:
  - `type Scheduler struct { Home string; Logf func(string, ...any); Now func() time.Time; Run func(ctx context.Context, home string) error }`
  - `func NewScheduler(home string) *Scheduler` — real clock, real `Run`, stamped stderr logging
  - `func (s *Scheduler) Loop(ctx context.Context, ticks <-chan time.Time)` — returns when ctx ends
  - `func nextDue(start, lastOK time.Time, hasLast bool, every time.Duration) time.Time`
  - `func DoctorCheck(home string, now time.Time) (ok bool, detail string)`
  - `const startFloor = 2 * time.Minute`, `const failRetry = 15 * time.Minute`

Design note: the run executes synchronously inside the scheduler loop. The loop is already its own goroutine next to the watcher, so nothing else waits on it, and a run can never overlap itself. A manual `lossless backup` during a scheduled run gets `ErrLocked` from the flock; a scheduled tick during a manual run gets `ErrLocked` and re-checks a minute later. Task 10 records this in the spec.

- [ ] **Step 1: Write the failing scheduler tests**

```go
// internal/backup/schedule_test.go
package backup

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fakeClock struct{ t time.Time }

func (c *fakeClock) now() time.Time { return c.t }

// drive sends one tick per step and lets the loop process it before the
// next; runAt records the fake times at which Run fired.
type harness struct {
	clock  *fakeClock
	ticks  chan time.Time
	runAt  []time.Time
	runErr error
	done   chan struct{}
	s      *Scheduler
}

func newHarness(t *testing.T, home string, start time.Time) *harness {
	t.Helper()
	h := &harness{clock: &fakeClock{t: start}, ticks: make(chan time.Time), done: make(chan struct{})}
	stepDone := make(chan struct{}, 1)
	h.s = &Scheduler{
		Home: home,
		Logf: func(string, ...any) {},
		Now:  h.clock.now,
		Run: func(context.Context, string) error {
			h.runAt = append(h.runAt, h.clock.t)
			return h.runErr
		},
		tickDone: stepDone,
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() { h.s.Loop(ctx, h.ticks); close(h.done) }()
	return h
}

func (h *harness) tickAt(offset time.Duration) {
	h.clock.t = h.clock.t.Add(offset)
	h.ticks <- h.clock.t
	<-h.s.tickDone
}

func scheduledHome(t *testing.T, every time.Duration, lastOK string) string {
	t.Helper()
	clearBackupEnv(t)
	t.Setenv("LOSSLESS_BACKUP_ACCESS_KEY", "a")
	t.Setenv("LOSSLESS_BACKUP_SECRET_KEY", "s")
	home := t.TempDir()
	must(t, Init(home, InitOptions{URL: "s3://b", Every: every}))
	if lastOK != "" {
		st := LoadState(home)
		st.LastOK = lastOK
		must(t, st.Save(home))
	}
	return home
}

func TestNextDue(t *testing.T) {
	start := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)
	if d := nextDue(start, time.Time{}, false, time.Hour); !d.Equal(start.Add(2 * time.Minute)) {
		t.Fatal("no last_ok: floor", d)
	}
	if d := nextDue(start, start.Add(-3*time.Hour), true, time.Hour); !d.Equal(start.Add(2 * time.Minute)) {
		t.Fatal("overdue: floor", d)
	}
	if d := nextDue(start, start.Add(-50*time.Minute), true, time.Hour); !d.Equal(start.Add(10 * time.Minute)) {
		t.Fatal("fresh: last_ok + every", d)
	}
}

func TestSchedulerRunsAtDueTime(t *testing.T) {
	start := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)
	home := scheduledHome(t, time.Hour, start.Add(-50*time.Minute).Format(time.RFC3339))
	h := newHarness(t, home, start)
	h.tickAt(time.Minute) // 12:01, due 12:10
	h.tickAt(8 * time.Minute)
	if len(h.runAt) != 0 {
		t.Fatalf("ran early: %v", h.runAt)
	}
	h.tickAt(2 * time.Minute) // 12:11
	if len(h.runAt) != 1 {
		t.Fatalf("want one run, got %v", h.runAt)
	}
	h.tickAt(30 * time.Minute) // 12:41, next due 13:11
	if len(h.runAt) != 1 {
		t.Fatal("ran before the interval elapsed")
	}
	h.tickAt(31 * time.Minute) // 13:12
	if len(h.runAt) != 2 {
		t.Fatalf("want two runs, got %v", h.runAt)
	}
}

func TestSchedulerOverdueWaitsForFloor(t *testing.T) {
	start := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)
	home := scheduledHome(t, time.Hour, "")
	h := newHarness(t, home, start)
	h.tickAt(time.Minute)
	if len(h.runAt) != 0 {
		t.Fatal("must not run before the two-minute floor")
	}
	h.tickAt(2 * time.Minute)
	if len(h.runAt) != 1 || !h.runAt[0].Equal(start.Add(3*time.Minute)) {
		t.Fatalf("%v", h.runAt)
	}
}

func TestSchedulerFailureRetriesSoonAndLockedRetriesNextMinute(t *testing.T) {
	start := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)
	home := scheduledHome(t, time.Hour, "")
	h := newHarness(t, home, start)
	h.runErr = errors.New("boom")
	h.tickAt(3 * time.Minute) // runs, fails
	h.tickAt(14 * time.Minute)
	if len(h.runAt) != 1 {
		t.Fatal("failed run must not retry before 15m")
	}
	h.tickAt(2 * time.Minute) // 12:19 ≥ 12:18
	if len(h.runAt) != 2 {
		t.Fatalf("want retry after 15m, got %v", h.runAt)
	}
	h.runErr = ErrLocked
	h.tickAt(16 * time.Minute) // 12:35, runs, locked
	h.tickAt(time.Minute)      // 12:36: locked means try again next minute
	if len(h.runAt) != 4 {
		t.Fatalf("locked must retry next tick, got %v", h.runAt)
	}
}

func TestSchedulerEveryZeroNeverRuns(t *testing.T) {
	start := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)
	home := scheduledHome(t, 0, "")
	h := newHarness(t, home, start)
	for i := 0; i < 5; i++ {
		h.tickAt(time.Hour)
	}
	if len(h.runAt) != 0 {
		t.Fatal(h.runAt)
	}
}

func TestSchedulerPicksUpLaterInit(t *testing.T) {
	start := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)
	clearBackupEnv(t)
	home := t.TempDir()
	h := newHarness(t, home, start)
	h.tickAt(time.Hour)
	if len(h.runAt) != 0 {
		t.Fatal("unconfigured must not run")
	}
	t.Setenv("LOSSLESS_BACKUP_ACCESS_KEY", "a")
	t.Setenv("LOSSLESS_BACKUP_SECRET_KEY", "s")
	must(t, Init(home, InitOptions{URL: "s3://b", Every: time.Hour}))
	h.tickAt(time.Minute) // configured now; floor starts here
	h.tickAt(3 * time.Minute)
	if len(h.runAt) != 1 {
		t.Fatalf("a daemon that was up before init must start backing up: %v", h.runAt)
	}
}
```

- [ ] **Step 2: Write the failing doctor test**

```go
// internal/backup/doctor_test.go
package backup

import (
	"strings"
	"testing"
	"time"
)

func TestDoctorCheck(t *testing.T) {
	now := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)
	clearBackupEnv(t)
	if ok, d := DoctorCheck(t.TempDir(), now); !ok || d != "not configured" {
		t.Fatal(ok, d)
	}
	home := scheduledHome(t, time.Hour, "")
	if ok, d := DoctorCheck(home, now); ok || !strings.Contains(d, "never") {
		t.Fatal(ok, d)
	}
	st := LoadState(home)
	st.LastOK = now.Add(-12 * time.Minute).Format(time.RFC3339)
	must(t, st.Save(home))
	ok, d := DoctorCheck(home, now)
	if !ok || !strings.Contains(d, "s3://b") || !strings.Contains(d, "encrypted") || !strings.Contains(d, "keep=5") || !strings.Contains(d, "12m") || !strings.Contains(d, "due in 48m") {
		t.Fatal(ok, d)
	}
	st.LastOK = now.Add(-3 * time.Hour).Format(time.RFC3339)
	must(t, st.Save(home))
	if ok, d := DoctorCheck(home, now); ok || !strings.Contains(d, "stale") {
		t.Fatal(ok, d)
	}
	st.LastOK = now.Add(-12 * time.Minute).Format(time.RFC3339)
	st.LastAttempt = now.Add(-2 * time.Minute).Format(time.RFC3339)
	st.LastError = "s3 PUT manifest: 503"
	must(t, st.Save(home))
	if ok, d := DoctorCheck(home, now); ok || !strings.Contains(d, "last attempt failed: s3 PUT manifest: 503") {
		t.Fatal(ok, d)
	}
	// Schedule off: only "never" is a failure.
	off := scheduledHome(t, 0, now.Add(-30*24*time.Hour).Format(time.RFC3339))
	if ok, d := DoctorCheck(off, now); !ok || !strings.Contains(d, "schedule off") {
		t.Fatal(ok, d)
	}
}
```

- [ ] **Step 3: Run them to verify they fail**

Run: `go test ./internal/backup/ -run 'TestNextDue|TestScheduler|TestDoctor' -v`
Expected: FAIL — undefined: Scheduler, nextDue, DoctorCheck

- [ ] **Step 4: Write schedule.go**

```go
// internal/backup/schedule.go
package backup

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"time"
)

const (
	startFloor = 2 * time.Minute
	failRetry  = 15 * time.Minute
)

// Scheduler runs backups inside serve --watch on a due time derived from
// the last success, so a daemon that restarts often still backs up and a
// laptop that sleeps past its due time backs up soon after waking.
type Scheduler struct {
	Home string
	Logf func(format string, args ...any)
	Now  func() time.Time
	Run  func(ctx context.Context, home string) error

	tickDone chan struct{} // tests: closed-loop stepping
}

func NewScheduler(home string) *Scheduler {
	return &Scheduler{
		Home: home,
		Logf: func(format string, args ...any) {
			fmt.Fprintf(os.Stderr, time.Now().UTC().Format(time.RFC3339)+" lossless backup: "+format+"\n", args...)
		},
		Now: time.Now,
		Run: func(ctx context.Context, home string) error {
			sum, err := Run(ctx, home, RunOptions{Out: io.Discard})
			if err != nil {
				return err
			}
			if sum.NoChange {
				fmt.Fprintf(os.Stderr, "%s lossless backup: no change (%d files, %s)\n", time.Now().UTC().Format(time.RFC3339), sum.Scanned, sum.Elapsed.Round(time.Millisecond))
			} else {
				fmt.Fprintf(os.Stderr, "%s lossless backup: generation %s uploaded %d (%d bytes) deleted %d dropped %v (%s)\n",
					time.Now().UTC().Format(time.RFC3339), sum.Generation, sum.Uploaded, sum.Bytes, sum.Deleted, sum.Dropped, sum.Elapsed.Round(time.Millisecond))
			}
			return nil
		},
	}
}

func nextDue(start, lastOK time.Time, hasLast bool, every time.Duration) time.Time {
	floor := start.Add(startFloor)
	if !hasLast {
		return floor
	}
	if due := lastOK.Add(every); due.After(floor) {
		return due
	}
	return floor
}

func minDuration(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}

// Loop reads backup.env on every tick, so a later `backup init` or an
// edited interval takes effect within a minute. Ticks before the due time
// do nothing. The run is synchronous: a tick can never overlap a run.
func (s *Scheduler) Loop(ctx context.Context, ticks <-chan time.Time) {
	var (
		due        time.Time
		configured bool
	)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticks:
			s.step(&due, &configured)
			if s.tickDone != nil {
				s.tickDone <- struct{}{}
			}
		}
	}
}

func (s *Scheduler) step(due *time.Time, configured *bool) {
	cfg, err := LoadConfig(s.Home)
	if err != nil || cfg.Every <= 0 {
		if err != nil && !errors.Is(err, ErrNotConfigured) {
			s.Logf("config: %v", err)
		}
		*configured = false
		return
	}
	now := s.Now()
	if !*configured {
		last, has := LoadState(s.Home).LastOKTime()
		*due = nextDue(now, last, has, cfg.Every)
		*configured = true
	}
	if now.Before(*due) {
		return
	}
	err = s.Run(context.Background(), s.Home)
	now = s.Now()
	switch {
	case err == nil:
		*due = now.Add(cfg.Every)
	case errors.Is(err, ErrLocked):
		s.Logf("manual run in progress; retrying next minute")
		*due = now.Add(time.Minute)
	default:
		s.Logf("%v", err)
		*due = now.Add(minDuration(failRetry, cfg.Every))
	}
}
```

- [ ] **Step 5: Write doctor.go**

```go
// internal/backup/doctor.go
package backup

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// DoctorCheck is the one line `lossless doctor` prints for backup.
func DoctorCheck(home string, now time.Time) (ok bool, detail string) {
	cfg, err := LoadConfig(home)
	if errors.Is(err, ErrNotConfigured) {
		return true, "not configured"
	}
	if err != nil {
		return false, err.Error()
	}
	st := LoadState(home)
	var b strings.Builder
	fmt.Fprintf(&b, "%s encrypted keep=%d", cfg.URL, cfg.Keep)
	if cfg.Every <= 0 {
		b.WriteString(" schedule off")
	}
	last, has := st.LastOKTime()
	ok = true
	if !has {
		b.WriteString(" never backed up")
		ok = false
		if cfg.Every <= 0 {
			b.WriteString(" (run lossless backup)")
		}
	} else {
		age := now.Sub(last)
		fmt.Fprintf(&b, " last ok %s ago", age.Round(time.Minute))
		if cfg.Every > 0 && age > 2*cfg.Every {
			b.WriteString(" (stale)")
			ok = false
		}
	}
	if cfg.Every > 0 {
		if !has {
			fmt.Fprintf(&b, ", due within %s of serve start", startFloor)
		} else if due := last.Add(cfg.Every); due.After(now) {
			fmt.Fprintf(&b, ", due in %s", due.Sub(now).Round(time.Minute))
		} else {
			b.WriteString(", due now")
		}
	}
	if st.LastError != "" && st.LastAttempt > st.LastOK {
		fmt.Fprintf(&b, "; last attempt failed: %s", st.LastError)
		ok = false
	}
	return ok, b.String()
}
```

- [ ] **Step 6: Run the scheduler and doctor tests**

Run: `go test ./internal/backup/ -run 'TestNextDue|TestScheduler|TestDoctor' -v`
Expected: PASS

- [ ] **Step 7: Start the scheduler in serve.Listen**

In `internal/serve/serve.go`, add `"lossless/internal/backup"` to the imports and change the watch block to:

```go
	if opts.Watch {
		wopts := watch.Defaults()
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go func() { _ = watch.Run(ctx, st, wopts) }()
		go func() {
			t := time.NewTicker(time.Minute)
			defer t.Stop()
			backup.NewScheduler(st.Root).Loop(ctx, t.C)
		}()
	}
```

- [ ] **Step 8: Run the whole suite**

Run: `go build ./... && go test ./...`
Expected: PASS. The existing serve test that starts `Listen` with `Watch: true` still passes: its temp store has no `backup.env`, so every scheduler tick is a no-op.

- [ ] **Step 9: gofmt, vet, commit**

```bash
gofmt -l internal && go vet ./... && \
git add internal/backup internal/serve/serve.go && \
git commit -m "Add backup scheduler, doctor check, and serve wiring."
```

---

### Task 9: CLI — backup init, backup, restore, doctor line, usage

**Files:**
- Create: `cmd/lossless/backup.go`
- Modify: `cmd/lossless/main.go` (switch at lines 45-90, `usage()` at line 98, `runDoctor` at line 535)
- Test: `cmd/lossless/backup_test.go`

**Interfaces:**
- Consumes: `backup.Init`, `backup.InitOptions`, `backup.Run`, `backup.RunOptions`, `backup.Restore`, `backup.RestoreOptions`, `backup.List`, `backup.DoctorCheck`, `backup.ConfigError`; `homeFlag`; `harness.Check`.
- Produces: `func runBackup(args []string) int`, `func runBackupInit(args []string) int`, `func runRestore(args []string) int`.

- [ ] **Step 1: Write the failing CLI test**

```go
// cmd/lossless/backup_test.go
package main

import (
	"os"
	"path/filepath"
	"testing"

	"lossless/internal/backup/s3/s3test"
	"lossless/internal/store"
)

func TestBackupInitRunRestoreList(t *testing.T) {
	for _, k := range []string{"LOSSLESS_BACKUP_URL", "LOSSLESS_BACKUP_ENDPOINT", "LOSSLESS_BACKUP_EVERY", "LOSSLESS_BACKUP_KEEP", "AWS_ACCESS_KEY_ID", "AWS_SECRET_ACCESS_KEY", "LOSSLESS_CLIENT"} {
		t.Setenv(k, "")
	}
	t.Setenv("LOSSLESS_BACKUP_ACCESS_KEY", "test")
	t.Setenv("LOSSLESS_BACKUP_SECRET_KEY", "secret")
	t.Setenv("LOSSLESS_SIDECAR", "off")
	srv := s3test.New()
	defer srv.Close()

	home := t.TempDir()
	st, err := store.Open(home)
	if err != nil {
		t.Fatal(err)
	}
	_ = st.Close()

	if runBackup([]string{"init"}) != 2 {
		t.Fatal("init without a URL must exit 2")
	}
	if runBackup([]string{"init", "--home", home, "--endpoint", srv.URL(), "--every", "30m", "--keep", "3", "s3://bkt/pre"}) != 0 {
		t.Fatal("init")
	}
	for _, f := range []string{"backup.env", "backup.key"} {
		if _, err := os.Stat(filepath.Join(home, f)); err != nil {
			t.Fatal(err)
		}
	}
	if runBackup([]string{"init", "--home", home, "s3://bkt/pre"}) != 1 {
		t.Fatal("second init must refuse to replace the key")
	}
	if runBackup([]string{"--home", home, "--dry-run"}) != 0 {
		t.Fatal("dry run")
	}
	if srv.Count("PUT") != 0 {
		t.Fatal("dry run wrote")
	}
	if runBackup([]string{"--home", home}) != 0 {
		t.Fatal("backup")
	}
	if srv.Count("PUT") == 0 {
		t.Fatal("nothing uploaded")
	}
	if runRestore([]string{"--home", home, "--list"}) != 0 {
		t.Fatal("list")
	}
	fresh := t.TempDir()
	st2, err := store.Open(fresh)
	if err != nil {
		t.Fatal(err)
	}
	_ = st2.Close()
	for _, f := range []string{"backup.env", "backup.key"} {
		b, _ := os.ReadFile(filepath.Join(home, f))
		if err := os.WriteFile(filepath.Join(fresh, f), b, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if runRestore([]string{"--home", fresh}) != 0 {
		t.Fatal("restore")
	}
	if _, err := os.Stat(filepath.Join(fresh, "index", "claims.sqlite")); err != nil {
		t.Fatal(err)
	}
	if runRestore([]string{"--home", fresh, "--at", "nope"}) != 1 {
		t.Fatal("unknown generation must exit 1")
	}
	if runBackup([]string{"--home", t.TempDir()}) != 2 {
		t.Fatal("unconfigured home must exit 2")
	}
	if runBackup([]string{"-bogus"}) != 2 || runRestore([]string{"-bogus"}) != 2 {
		t.Fatal("bad flags must exit 2")
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./cmd/lossless/ -run TestBackupInit -v`
Expected: FAIL — undefined: runBackup, runRestore

- [ ] **Step 3: Write cmd/lossless/backup.go**

```go
// cmd/lossless/backup.go
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"lossless/internal/backup"
)

func runBackup(args []string) int {
	if len(args) > 0 && args[0] == "init" {
		return runBackupInit(args[1:])
	}
	fs := flag.NewFlagSet("backup", flag.ContinueOnError)
	home := homeFlag(fs)
	dry := fs.Bool("dry-run", false, "plan only; write nothing to the bucket")
	verbose := fs.Bool("verbose", false, "print each uploaded file")
	takeOver := fs.Bool("take-over", false, "adopt the install that last wrote the bucket")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	sum, err := backup.Run(context.Background(), *home, backup.RunOptions{DryRun: *dry, Verbose: *verbose, TakeOver: *takeOver, Out: os.Stdout})
	if err != nil {
		fmt.Fprintln(os.Stderr, "lossless backup:", err)
		var ce *backup.ConfigError
		if errors.As(err, &ce) {
			return 2
		}
		return 1
	}
	switch {
	case sum.DryRun:
		fmt.Printf("dry run: scanned %d files\n", sum.Scanned)
	case sum.NoChange:
		fmt.Printf("backup: no change (scanned %d files) in %s\n", sum.Scanned, sum.Elapsed.Round(time.Millisecond))
	default:
		fmt.Printf("backup: generation %s: scanned %d, uploaded %d (%d bytes), deleted %d", sum.Generation, sum.Scanned, sum.Uploaded, sum.Bytes, sum.Deleted)
		if len(sum.Dropped) > 0 {
			fmt.Printf(", dropped %s", strings.Join(sum.Dropped, " "))
		}
		fmt.Printf(", in %s\n", sum.Elapsed.Round(time.Millisecond))
	}
	return 0
}

func runBackupInit(args []string) int {
	fs := flag.NewFlagSet("backup init", flag.ContinueOnError)
	home := homeFlag(fs)
	endpoint := fs.String("endpoint", "", "S3-compatible endpoint URL (R2, B2, MinIO); empty means AWS")
	region := fs.String("region", "", "signing region (default us-east-1; an R2 endpoint signs auto by itself)")
	every := fs.Duration("every", time.Hour, "schedule interval inside serve --watch; 0 disables")
	keep := fs.Int("keep", 5, "generations to keep in the bucket")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	target := fs.Arg(0)
	if target == "" {
		fmt.Fprintln(os.Stderr, "usage: lossless backup init s3://<bucket>/<prefix> [--endpoint URL] [--region R] [--every 1h] [--keep 5]")
		return 2
	}
	if err := backup.Init(*home, backup.InitOptions{URL: target, Endpoint: *endpoint, Region: *region, Every: *every, Keep: *keep}); err != nil {
		fmt.Fprintln(os.Stderr, "lossless backup init:", err)
		return 1
	}
	fmt.Printf("wrote %s and %s\n", filepath.Join(*home, "backup.env"), filepath.Join(*home, "backup.key"))
	fmt.Println("Copy backup.key somewhere that is not this machine. Restore is impossible without it.")
	if os.Getenv("LOSSLESS_BACKUP_ACCESS_KEY") == "" && os.Getenv("AWS_ACCESS_KEY_ID") == "" {
		fmt.Println("Add LOSSLESS_BACKUP_ACCESS_KEY and LOSSLESS_BACKUP_SECRET_KEY to backup.env before the first run.")
	}
	if *every > 0 {
		fmt.Printf("Schedule: every %s while lossless serve runs (picked up within a minute). Run `lossless backup` now for the first copy.\n", *every)
	} else {
		fmt.Println("Schedule: off. Run `lossless backup` yourself.")
	}
	return 0
}

func runRestore(args []string) int {
	fs := flag.NewFlagSet("restore", flag.ContinueOnError)
	home := homeFlag(fs)
	force := fs.Bool("force", false, "union into a non-empty store; index snapshots are replaced, differing raw/export files are left alone")
	at := fs.String("at", "", "generation id from --list (default: latest)")
	list := fs.Bool("list", false, "print kept generations and exit")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	ctx := context.Background()
	if *list {
		gens, err := backup.List(ctx, *home)
		if err != nil {
			fmt.Fprintln(os.Stderr, "lossless restore:", err)
			return 1
		}
		fmt.Printf("%-22s %-20s %-8s %6s %12s\n", "GENERATION", "CREATED", "VERSION", "FILES", "BYTES")
		for _, g := range gens {
			mark := ""
			if g.Latest {
				mark = "  (latest)"
			}
			fmt.Printf("%-22s %-20s %-8s %6d %12d%s\n", g.Generation, g.CreatedAt, g.Lossless, g.Files, g.Bytes, mark)
		}
		return 0
	}
	sum, err := backup.Restore(ctx, *home, backup.RestoreOptions{Force: *force, At: *at, Out: os.Stderr})
	if err != nil {
		fmt.Fprintln(os.Stderr, "lossless restore:", err)
		var ce *backup.ConfigError
		if errors.As(err, &ce) {
			return 2
		}
		return 1
	}
	fmt.Printf("restore: generation %s: restored %d (%d bytes), skipped %d, left alone %d\n", sum.Generation, sum.Restored, sum.Bytes, sum.Skipped, sum.LeftAlone)
	for _, rel := range sum.Left {
		fmt.Printf("left alone (differs locally): %s\n", rel)
	}
	fmt.Println("Start lossless serve (or lossless setup on a new machine) to resume.")
	return 0
}
```

- [ ] **Step 4: Wire main.go**

Add two cases to the switch in `main()` next to `case "ensure":`:

```go
	case "backup":
		os.Exit(runBackup(args))
	case "restore":
		os.Exit(runRestore(args))
```

Add these lines to `usage()` after the `lossless ensure` line:

```
  lossless backup init s3://B/P [--endpoint URL] [--every 1h] [--keep 5]   # opt-in encrypted copy to a bucket you own
  lossless backup [--dry-run] [--take-over]   # one incremental run; serve --watch runs it on the schedule
  lossless restore [--force] [--at GEN] [--list]   # pull a generation back into an empty store
```

And in the `Env:` block add:

```
     LOSSLESS_BACKUP_* live in ~/.lossless/backup.env (written by backup init); backup.key is the encryption key
```

In `runDoctor`, after `rep := harness.Doctor(...)` and before `fmt.Print(rep.Format())`:

```go
	bok, bdetail := backup.DoctorCheck(*home, time.Now())
	rep.Checks = append(rep.Checks, harness.Check{Name: "backup", OK: bok, Detail: bdetail})
```

Add `"lossless/internal/backup"` to main.go's imports (and `"time"` if it is not already there; it is, from `runServe`).

- [ ] **Step 5: Run the CLI tests and the whole suite**

Run: `go test ./cmd/lossless/ -run 'TestBackupInit|TestUsage' -v && go test ./...`
Expected: PASS. `TestUsageAndHomeFlag` still passes because it only checks that usage prints.

- [ ] **Step 6: gofmt, vet, commit**

```bash
gofmt -l cmd internal && go vet ./... && \
git add cmd/lossless/backup.go cmd/lossless/backup_test.go cmd/lossless/main.go && \
git commit -m "Add backup, backup init, and restore commands; doctor backup line."
```

---

### Task 10: Docs, spec amendments, live R2 test

**Files:**
- Modify: `docs/deploy.md` (new section before `## Security`; out-of-scope list at the end)
- Modify: `docs/roadmap.md:165` and the operator list above it
- Modify: `README.md:39` (one bullet after "Local by default")
- Modify: `CHANGELOG.md` (new top section)
- Modify: `docs/superpowers/specs/2026-09-12-object-storage-backup-design.md` (three implementation notes)
- Create: `internal/backup/live_test.go`

**Interfaces:**
- Consumes: everything above. No new code interfaces.

- [ ] **Step 1: Add the deploy.md section**

Insert this before the `## Security (the parts we own)` heading:

```markdown
## Back up to object storage (optional)

lossless keeps running on local files. This copies the store to an
S3-compatible bucket you own, encrypted with a key you hold, on a schedule.
It is not a home: `ask` never reads the bucket, and nothing here changes the
loopback default. Default install still uploads nothing.

```bash
export LOSSLESS_BACKUP_ACCESS_KEY=…   # or AWS_ACCESS_KEY_ID / AWS_SECRET_ACCESS_KEY
export LOSSLESS_BACKUP_SECRET_KEY=…
lossless backup init s3://my-bucket/lossless --endpoint https://<account>.r2.cloudflarestorage.com
lossless backup            # first copy now; serve --watch repeats it hourly
lossless doctor            # "backup   ok   s3://… encrypted keep=5 last ok 3m ago, due in 57m"
```

`backup init` writes `~/.lossless/backup.env` and `~/.lossless/backup.key`
(both `0600`). **Copy `backup.key` somewhere that is not this machine.**
Restore is impossible without it. Credentials present in the environment at
init time are written into `backup.env`; otherwise add them there.

### What is copied

| Path | How |
|------|-----|
| `raw/**/*.jsonl.zst` | Sealed tape parts. Uploaded once, never re-read. |
| `raw/**/*.jsonl` | Live parts. Copied under a shared lock, re-uploaded as they grow. |
| `export/**/*.md` | Claims. |
| `index/*.sqlite` | `VACUUM INTO` snapshots. Restore needs no rebuild. |

Nothing else in the home is read. `spool/`, `active/`, `serve.log`,
`service.env`, and the backup files themselves stay on this machine.

Every object is chunked AES-256-GCM under a per-object key derived from
`backup.key`. Object names are an HMAC of the path. The bucket shows no
project names, session ids, or paths. Someone with bucket read access learns
nothing; someone with write access can delete or roll back, and a rollback
shows in `restore --list`.

### Generations

Each run that changed anything writes a full manifest and moves the
pointer. The last five generations are kept (`--keep`), and an object is
deleted only when no kept generation references it. A run with no changes
writes nothing. Bucket versioning is not assumed; Cloudflare R2 does not
implement it.

```bash
lossless restore --list
lossless restore --at 20260914T151500Z-a1f3
```

### Schedule

`serve --watch` runs a backup when one is due: last success plus the
interval, or two minutes after the daemon starts if that is already past.
A daemon that restarts daily with a daily interval still backs up. A failed
run retries in fifteen minutes. `--every 0` at init turns the schedule off;
`lossless backup` is always available by hand. `doctor` warns when the last
success is older than twice the interval, or when the last attempt failed.

### Restore

On a new machine: install lossless, run `lossless setup`, copy `backup.env`
and `backup.key` into `~/.lossless`, stop the daemon, then:

```bash
lossless restore           # latest generation into an empty store
lossless setup             # or just start serve again
```

`restore` refuses a running daemon and a non-empty store. `--force` unions
into a non-empty store: index snapshots are replaced, raw and export files
that differ locally are left alone and listed. The restored machine becomes
the bucket's writer; if the old machine comes back it refuses to back up
until you run `lossless backup --take-over` there on purpose.

### Cloudflare R2

- Endpoint `https://<account>.r2.cloudflarestorage.com`, or
  `https://<account>.<jurisdiction>.r2.cloudflarestorage.com` for a bucket
  created in a jurisdiction.
- Create an R2 API token with **Object Read & Write** scoped to the bucket.
  Cloudflare shows the S3 Access Key ID and Secret Access Key once, on the
  token page. Those are the two credentials above.
- Region is `auto`; the client sets it for any R2 endpoint.
- R2 has no bucket versioning. Generations are the history.

Any other S3-compatible store works the same way with its endpoint. AWS
needs no `--endpoint`.
```

Then change the out-of-scope bullet `- S3 as the raw store` to:

```markdown
- S3 as the store `ask` reads from. A bucket is a copy (`lossless backup`), never a home.
```

- [ ] **Step 2: Update roadmap.md**

Change line 165 from:

```
Remote home stays manual: TLS + token + local sidecar. No cloud image, no org ACL, no S3.
```

to:

```
Remote home stays manual: TLS + token + local sidecar. No cloud image, no org ACL, no S3 as the store. `backup` copies the store to a bucket you own (encrypted, kept generations, restore); `ask` still reads local files.
```

And add to the operator list above it:

```
- `backup` / `restore` to an S3-compatible bucket (0.1.26): encrypted per-file mirror, five kept generations, hourly by default inside `serve --watch`
```

- [ ] **Step 3: Update README.md**

After the "Local by default" bullet at line 39, add:

```markdown
- **Backup is yours.** Optional `lossless backup` copies the store to an S3-compatible bucket you own, encrypted with a key you hold. `ask` still reads local files. See [docs/deploy.md](docs/deploy.md).
```

- [ ] **Step 4: Add the CHANGELOG entry**

Insert at the top of `CHANGELOG.md` under `# Changelog`:

```markdown
## 0.1.26 — unreleased

- `backup init`, `backup`, and `restore`: opt-in encrypted copy of `raw/`, `export/`, and `VACUUM INTO` index snapshots to an S3-compatible bucket (AWS, Cloudflare R2, B2, MinIO). Per-file objects under HMAC names, chunked AES-256-GCM with a per-object key, manifest written last, the last five generations kept and objects deleted only when no kept generation references them. `restore --list` and `restore --at` pick a generation. A writer guard refuses a bucket last written by another install; `restore` adopts it, `--take-over` overrides.
- `serve --watch` runs backup on a due time from the last success (two minutes after start if overdue, retry in fifteen minutes on failure); `backup init` schedules hourly by default. `doctor` prints the backup line.
- Hand-rolled SigV4 client with signed payloads and region `auto` on R2 hosts; no new modules.
- Pack of five and 4.0 / 2.5 unchanged.
```

- [ ] **Step 5: Record three implementation notes in the spec**

In `docs/superpowers/specs/2026-09-12-object-storage-backup-design.md`:

1. In "Backup run" step 7, replace `4 in flight.` with `Deletes run sequentially; a dropped generation owns a handful of unique objects.`
2. In "Scheduling", replace the paragraph starting `The run goes in its own goroutine.` with: `The scheduler is its own goroutine started next to the watcher in serve.Listen; the run executes synchronously inside it, so a tick can never overlap a run. A manual backup during a scheduled run gets "backup already running"; a scheduled tick during a manual run sees the same and re-checks a minute later. backup.env is re-read every tick, so a later backup init or an edited interval takes effect within a minute. Errors go to serve.log with the time and version stamp. Catch-up and ask never wait on it.`
3. In "Packages", replace `internal/watch/watch.go  backup ticker` with `internal/serve/serve.go   start the scheduler goroutine next to the watcher` and change the sentence `watch imports backup` to `serve imports backup`.
4. In "Surface", after the `backup init` paragraph add: `Credentials present in the environment at init time (LOSSLESS_BACKUP_* or AWS_*) are written into backup.env; otherwise commented placeholders are written and the command says so.`

- [ ] **Step 6: Write the live test**

```go
// internal/backup/live_test.go
package backup

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"lossless/internal/backup/s3"
)

// TestLiveR2 runs a real backup and restore against a bucket you own.
// Skipped unless LOSSLESS_BACKUP_LIVE_TEST=1 and the usual LOSSLESS_BACKUP_*
// variables point at the bucket. Everything it writes goes under a random
// prefix and is deleted at the end.
//
//   LOSSLESS_BACKUP_LIVE_TEST=1 LOSSLESS_BACKUP_URL=s3://bkt/lossless-test \
//   LOSSLESS_BACKUP_ENDPOINT=https://<acct>.r2.cloudflarestorage.com \
//   LOSSLESS_BACKUP_ACCESS_KEY=… LOSSLESS_BACKUP_SECRET_KEY=… \
//   go test ./internal/backup/ -run TestLiveR2 -v
func TestLiveR2(t *testing.T) {
	if os.Getenv("LOSSLESS_BACKUP_LIVE_TEST") != "1" {
		t.Skip("set LOSSLESS_BACKUP_LIVE_TEST=1 and LOSSLESS_BACKUP_* to run")
	}
	base := os.Getenv("LOSSLESS_BACKUP_URL")
	if base == "" {
		t.Fatal("LOSSLESS_BACKUP_URL is required")
	}
	url := strings.TrimRight(base, "/") + "/" + NewGeneration(time.Now())
	t.Setenv("LOSSLESS_BACKUP_URL", url)
	home, _ := seedHome(t)
	must(t, Init(home, InitOptions{URL: url, Endpoint: os.Getenv("LOSSLESS_BACKUP_ENDPOINT"), Region: os.Getenv("LOSSLESS_BACKUP_REGION"), Every: 0, Keep: 2}))
	ctx := context.Background()

	cleanup := func() {
		cfg, err := LoadConfig(home)
		if err != nil {
			return
		}
		rm, err := newRemote(cfg, home)
		if err != nil {
			return
		}
		pointer, err := rm.getManifest(ctx, pointerKey)
		if err != nil {
			return
		}
		for _, g := range append(append([]string{}, pointer.Generations...), pointer.Dropping...) {
			m, err := rm.getManifest(ctx, manifestKey(g))
			if err == nil {
				for _, e := range m.Files {
					_ = rm.c.Delete(ctx, e.Object)
				}
			}
			_ = rm.c.Delete(ctx, manifestKey(g))
		}
		for _, e := range pointer.Files {
			_ = rm.c.Delete(ctx, e.Object)
		}
		_ = rm.c.Delete(ctx, pointerKey)
	}
	t.Cleanup(cleanup)

	first := runOK(t, home, RunOptions{Verbose: true, Out: os.Stderr})
	if first.Uploaded == 0 {
		t.Fatal("nothing uploaded")
	}
	if again := runOK(t, home, RunOptions{}); !again.NoChange {
		t.Fatalf("second run must be no change: %+v", again)
	}
	touchLive(t, home, `{"role":"assistant","content":"live grew"}`)
	second := runOK(t, home, RunOptions{})
	touchLive(t, home, `{"role":"assistant","content":"live grew again"}`)
	third := runOK(t, home, RunOptions{})
	if strings.Join(third.Dropped, ",") != first.Generation {
		t.Fatalf("keep=2 must drop the first generation: %+v", third)
	}
	gens, err := List(ctx, home)
	if err != nil || len(gens) != 2 || gens[0].Generation != third.Generation || gens[1].Generation != second.Generation {
		t.Fatalf("%+v %v", gens, err)
	}

	dst := freshTarget(t, home)
	sum, err := Restore(ctx, dst, RestoreOptions{Health: noDaemon})
	if err != nil || sum.Restored != third.Scanned {
		t.Fatalf("%+v %v", sum, err)
	}
	if strings.Join(claimTexts(t, dst), "|") != strings.Join(claimTexts(t, home), "|") {
		t.Fatal("claims differ after live restore")
	}
	if _, err := s3.New(s3.Config{Bucket: "x", Endpoint: os.Getenv("LOSSLESS_BACKUP_ENDPOINT")}); err != nil {
		t.Fatal(err)
	}
}
```

- [ ] **Step 7: Run everything, including the live test against your R2 bucket**

Run: `gofmt -l . && go vet ./... && go test ./...`
Expected: PASS, `TestLiveR2` reported as SKIP.

Then, by hand, with a real R2 token scoped to a scratch bucket:

Run: `LOSSLESS_BACKUP_LIVE_TEST=1 LOSSLESS_BACKUP_URL=s3://<bucket>/lossless-live LOSSLESS_BACKUP_ENDPOINT=https://<account>.r2.cloudflarestorage.com LOSSLESS_BACKUP_ACCESS_KEY=… LOSSLESS_BACKUP_SECRET_KEY=… go test ./internal/backup/ -run TestLiveR2 -v`
Expected: PASS. If R2 answers `SignatureDoesNotMatch`, the region did not sign as `auto`; check `Config.normalized` against the endpoint host.

- [ ] **Step 8: Commit**

```bash
git add docs/deploy.md docs/roadmap.md README.md CHANGELOG.md docs/superpowers/specs/2026-09-12-object-storage-backup-design.md internal/backup/live_test.go && \
git commit -m "Document backup to object storage; add the live R2 test."
```

Shipping is outside this plan: the release follows the repo's convention of one `Ship 0.1.26: …` commit on `main` that bumps `internal/version/version.go`, plus the tag that `release.yml` builds from.
