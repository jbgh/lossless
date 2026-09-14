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
