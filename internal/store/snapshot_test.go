package store

import (
	"crypto/sha256"
	"database/sql"
	"os"
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
	// The live store keeps working after the snapshot.
	active, err := st.ListActive("acme/api")
	if err != nil || len(active) != 2 {
		t.Fatalf("live store: %d %v", len(active), err)
	}
}

// TestSnapshotIsByteStableAcrossOpen guards the property restore relies on:
// a snapshot, once taken, must not change bytes the first time something
// opens it live (as a restored machine's daemon inevitably will).
func TestSnapshotIsByteStableAcrossOpen(t *testing.T) {
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

	header, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if len(header) < 20 || header[18] != 2 || header[19] != 2 {
		t.Fatalf("snapshot must already be in WAL format: bytes 18,19 = %d,%d", header[18], header[19])
	}
	if _, err := os.Stat(dst + "-wal"); !os.IsNotExist(err) {
		t.Fatalf("snapshot must not leave a -wal sidecar: %v", err)
	}
	before := sha256.Sum256(header)

	newHome := t.TempDir()
	if err := os.MkdirAll(filepath.Join(newHome, "index"), 0o700); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(newHome, "index", "claims.sqlite"), b, 0o600); err != nil {
		t.Fatal(err)
	}
	restored, err := Open(newHome)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := restored.ListActive("acme/api"); err != nil {
		t.Fatal(err)
	}
	if err := restored.Close(); err != nil {
		t.Fatal(err)
	}

	after, err := os.ReadFile(filepath.Join(newHome, "index", "claims.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	if sha256.Sum256(after) != before {
		t.Fatal("opening the restored snapshot live changed its bytes")
	}
	if _, err := os.Stat(filepath.Join(newHome, "index", "claims.sqlite-wal")); !os.IsNotExist(err) {
		t.Fatalf("restored store must not leave a -wal sidecar after close: %v", err)
	}
}
