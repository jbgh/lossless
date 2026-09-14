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
