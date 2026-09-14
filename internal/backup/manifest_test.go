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
