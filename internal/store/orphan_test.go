package store

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	"lossless/internal/claim"
	"lossless/internal/projectkey"
)

func orphanStore(t *testing.T) *Store {
	t.Helper()
	st, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

// A lookup miss must not manufacture 13 empty monthly shard files.
func TestExcerptLookupMissDoesNotCreateShards(t *testing.T) {
	st := orphanStore(t)
	ref := &claim.TranscriptRef{SessionID: "nosuch", StartOffset: 0, EndOffset: 40}
	if _, ok := st.ExcerptCovering(ref, time.Time{}); ok {
		t.Fatal("unexpected excerpt hit")
	}
	shards, err := filepath.Glob(filepath.Join(st.Root, "index", "excerpts-*.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	if len(shards) != 0 {
		t.Fatalf("lookup miss created %d empty shards: %v", len(shards), shards)
	}
}

// Losing the insert race on idx_hash_active is an idempotent success —
// the winner holds the claim — but the loser must leave no orphan FTS
// row, export file, or vector behind.
func TestWriteClaimLostHashRaceLeavesNoOrphans(t *testing.T) {
	st := orphanStore(t)
	text := "Use jose, not jsonwebtoken, for Edge."
	winner := claim.Record{
		ID: claim.NewID(), Type: "decision", ProjectKey: "acme/api",
		Harness: "grok", SessionID: "s1", Source: "turn", Status: "active",
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
		Text:      text, ClaimHash: claim.Hash("acme/api", "decision", text),
	}
	testHookAfterHashCheck = func(tx *sql.Tx) {
		testHookAfterHashCheck = nil
		if err := upsertRecord(tx, winner); err != nil {
			t.Errorf("winner insert: %v", err)
		}
	}
	t.Cleanup(func() { testHookAfterHashCheck = nil })

	loser := winner
	loser.ID = claim.NewID()
	sup, err := st.WriteClaim(loser)
	if err != nil {
		t.Fatalf("lost race must be idempotent success, got %v", err)
	}
	if sup != "" {
		t.Fatalf("superseded=%q on lost race", sup)
	}
	if _, ok := st.Get(loser.ID); ok {
		t.Fatal("loser row should not exist")
	}
	export := filepath.Join(st.Root, "export", projectkey.Encode("acme/api"), loser.ID+".md")
	if _, err := os.Stat(export); !os.IsNotExist(err) {
		t.Fatalf("orphan export file written (stat err=%v)", err)
	}
	hits, err := st.SearchFTS("acme/api", `"jose"`, 10)
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range hits {
		if h.ID == loser.ID {
			t.Fatal("orphan FTS row for loser")
		}
	}
}
