package store

import (
	"strings"
	"testing"
)

// planUses reports whether SQLite's plan for q (with args) reads through
// the named index instead of scanning the table.
func planUses(t *testing.T, st *Store, index, q string, args ...any) bool {
	t.Helper()
	rows, err := st.DB.Query("EXPLAIN QUERY PLAN "+q, args...)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var plan []string
	for rows.Next() {
		var id, parent, notused int
		var detail string
		if err := rows.Scan(&id, &parent, &notused, &detail); err != nil {
			t.Fatal(err)
		}
		plan = append(plan, detail)
	}
	return strings.Contains(strings.Join(plan, "\n"), index)
}

// SessionByID runs on every remember and every ask that carries a session
// id; on a store with thousands of sessions it scanned the whole table.
func TestSessionByIDUsesAnIndex(t *testing.T) {
	st := orphanStore(t)
	if !planUses(t, st, "idx_sessions_sid",
		`SELECT jsonl FROM sessions WHERE session_id = ? ORDER BY rowid DESC LIMIT 1`, "x") {
		t.Fatal("SessionByID query does not use idx_sessions_sid")
	}
	if !planUses(t, st, "idx_sessions_proj",
		`SELECT jsonl FROM sessions WHERE project_key = ? ORDER BY rowid`, "o/r") {
		t.Fatal("SessionsForProject query does not use idx_sessions_proj")
	}
}

// SessionsForProject returns what filtering ListSessions by project
// returned, in the same (insertion) order.
func TestSessionsForProjectMatchesAFilteredList(t *testing.T) {
	st := orphanStore(t)
	rows := []Session{
		{JSONL: "/a/1.jsonl", SessionID: "1", Harness: "claude", Workspace: "/ws/a", Project: "Owner/Repo"},
		{JSONL: "/a/2.jsonl", SessionID: "2", Harness: "pi", Workspace: "/ws/b", Project: "other/repo"},
		{JSONL: "/a/3.jsonl", SessionID: "3", Harness: "codex", Workspace: "/ws/a", Project: "owner/repo"},
		{JSONL: "/a/4.jsonl", SessionID: "4", Harness: "grok", Workspace: "/ws/c"},
	}
	for _, s := range rows {
		if err := st.UpsertSession(s); err != nil {
			t.Fatal(err)
		}
	}
	got, err := st.SessionsForProject("owner/repo")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].SessionID != "1" || got[1].SessionID != "3" {
		t.Fatalf("got %+v, want sessions 1 and 3 in order", got)
	}
	if got, _ := st.SessionsForProject(""); len(got) != 0 {
		t.Fatalf("empty project matched %d rows", len(got))
	}
}

// A row an older release stored with a key in another form (before
// Normalize learned it) is still found by the project it normalizes to,
// as the old filter-in-Go found it.
func TestSessionsForProjectMatchesALegacyKey(t *testing.T) {
	st := orphanStore(t)
	if err := st.UpsertSession(Session{JSONL: "/a/new.jsonl", SessionID: "new", Harness: "claude", Project: "acme/api"}); err != nil {
		t.Fatal(err)
	}
	// Written raw, the way an older release would have stored it.
	if _, err := st.DB.Exec(`INSERT INTO sessions(jsonl, session_id, harness, workspace, project_key) VALUES('/a/old.jsonl','old','claude','','Acme/API.git')`); err != nil {
		t.Fatal(err)
	}
	got, err := st.SessionsForProject("acme/api")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].SessionID != "new" || got[1].SessionID != "old" {
		t.Fatalf("got %+v, want new then old", got)
	}
}
