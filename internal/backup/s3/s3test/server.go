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
