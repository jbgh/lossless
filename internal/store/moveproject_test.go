package store

import (
	"os"
	"path/filepath"
	"testing"

	"lossless/internal/claim"
)

func TestMoveProjectRehashesDedupesAndMovesSessions(t *testing.T) {
	st := tmp(t)
	put := func(id, project, text string) {
		t.Helper()
		r := rec(id, "decision", text, []string{"src/game.ts"})
		r.ProjectKey = project
		if _, err := st.WriteClaim(r); err != nil {
			t.Fatal(err)
		}
	}
	put("20261001000000aaaaaaaaaaaaaaaa", "acme/old-game", "Keep the day loop at eight minutes.")
	put("20261001000001bbbbbbbbbbbbbbbb", "acme/old-game", "Shops close at dusk.")
	put("20261020000000cccccccccccccccc", "acme/game", "Shops close at dusk.")
	if _, err := st.DB.Exec(`INSERT INTO sessions(jsonl, session_id, harness, workspace, project_key) VALUES('a.jsonl','s','claude','/w','acme/old-game')`); err != nil {
		t.Fatal(err)
	}

	dry, err := st.MoveProject("acme/old-game", "acme/game", true)
	if err != nil {
		t.Fatal(err)
	}
	if dry.Moved != 2 || dry.Active != 1 || dry.Deduped != 1 || dry.Sessions != 1 {
		t.Fatalf("dry run counts: %+v", dry)
	}
	if left, _ := st.ListActive("acme/old-game"); len(left) != 2 {
		t.Fatalf("dry run changed rows: %d left", len(left))
	}

	got, err := st.MoveProject("acme/old-game", "acme/game", false)
	if err != nil {
		t.Fatal(err)
	}
	if got != dry {
		t.Fatalf("real run %+v != dry run %+v", got, dry)
	}
	if left, _ := st.ListActive("acme/old-game"); len(left) != 0 {
		t.Fatalf("%d records left under the old key", len(left))
	}
	active, _ := st.ListActive("acme/game")
	if len(active) != 2 {
		t.Fatalf("want 2 active under the new key, got %d", len(active))
	}
	moved, _ := st.Get("20261001000000aaaaaaaaaaaaaaaa")
	if moved.ProjectKey != "acme/game" || moved.ClaimHash != claim.Hash("acme/game", "decision", moved.Text) {
		t.Fatalf("not rehashed: %+v", moved)
	}
	dup, _ := st.Get("20261001000001bbbbbbbbbbbbbbbb")
	if dup.Status != "superseded" {
		t.Fatalf("duplicate claim should be retired, got %s", dup.Status)
	}
	if kept, _ := st.Get("20261020000000cccccccccccccccc"); kept.Status != "active" {
		t.Fatal("the record already under the new key must stay active")
	}
	// Search and path postings follow the record.
	var n int
	_ = st.DB.QueryRow(`SELECT count(*) FROM path_postings WHERE project_key = 'acme/old-game'`).Scan(&n)
	if n != 0 {
		t.Fatalf("%d path postings left under the old key", n)
	}
	_ = st.DB.QueryRow(`SELECT count(*) FROM records_fts WHERE record_id = '20261001000001bbbbbbbbbbbbbbbb'`).Scan(&n)
	if n != 0 {
		t.Fatal("retired duplicate still searchable")
	}
	_ = st.DB.QueryRow(`SELECT count(*) FROM sessions WHERE project_key = 'acme/game'`).Scan(&n)
	if n != 1 {
		t.Fatal("session not moved")
	}
	// Exports move with the records.
	if _, err := os.Stat(filepath.Join(st.Root, "export", "acme__game", "20261001000000aaaaaaaaaaaaaaaa.md")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(st.Root, "export", "acme__old-game")); !os.IsNotExist(err) {
		t.Fatal("old export dir should be gone once empty")
	}
}

func TestMoveProjectRefusesSameKey(t *testing.T) {
	st := tmp(t)
	if _, err := st.MoveProject("acme/api", "ACME/api", false); err == nil {
		t.Fatal("same key after normalizing must be refused")
	}
	if _, err := st.MoveProject("", "acme/api", false); err == nil {
		t.Fatal("empty key must be refused")
	}
}

// An older release may have stored a key in another form. Those rows are
// the same project and move with it; left behind, a later backfill
// would write the old key back.
func TestMoveProjectMovesKeysStoredInAnOlderForm(t *testing.T) {
	st := tmp(t)
	r := rec("20261001000000aaaaaaaaaaaaaaaa", "decision", "Shops close at dusk.", []string{"src/game.ts"})
	r.ProjectKey = "acme/old-game"
	if _, err := st.WriteClaim(r); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`UPDATE records SET project_key = 'Acme/Old-Game'`,
		`UPDATE path_postings SET project_key = 'Acme/Old-Game'`,
		`INSERT INTO sessions(jsonl, session_id, harness, workspace, project_key) VALUES('a.jsonl','s','claude','/w','Acme/Old-Game')`,
	} {
		if _, err := st.DB.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	got, err := st.MoveProject("acme/old-game", "acme/game", false)
	if err != nil {
		t.Fatal(err)
	}
	if got.Moved != 1 || got.Active != 1 || got.Sessions != 1 {
		t.Fatalf("counts: %+v", got)
	}
	for _, table := range []string{"records", "path_postings", "sessions"} {
		var n int
		_ = st.DB.QueryRow(`SELECT count(*) FROM ` + table + ` WHERE project_key != 'acme/game'`).Scan(&n)
		if n != 0 {
			t.Fatalf("%d rows of %s left under the old key", n, table)
		}
	}
}

// A key nothing is stored under moves nothing and says so in its counts.
func TestMoveProjectMissingFromMovesNothing(t *testing.T) {
	st := tmp(t)
	got, err := st.MoveProject("acme/nothing", "acme/game", false)
	if err != nil || got.Moved != 0 || got.Sessions != 0 {
		t.Fatalf("%+v %v", got, err)
	}
}

func TestSessionInProjectPicksTheRowUnderThatProject(t *testing.T) {
	st := tmp(t)
	for _, s := range []Session{
		{JSONL: "/a/s.jsonl", SessionID: "s", Harness: "claude", Project: "acme/api"},
		{JSONL: "/b/s.jsonl", SessionID: "s", Harness: "grok", Project: "acme/web"},
	} {
		if err := st.UpsertSession(s); err != nil {
			t.Fatal(err)
		}
	}
	if got, ok := st.SessionInProject("s", "Acme/API"); !ok || got.Harness != "claude" {
		t.Fatalf("api row: %+v %v", got, ok)
	}
	if got, ok := st.SessionInProject("s", "acme/web"); !ok || got.Harness != "grok" {
		t.Fatalf("web row: %+v %v", got, ok)
	}
	if _, ok := st.SessionInProject("s", "acme/core"); ok {
		t.Fatal("no row under acme/core")
	}
}
