package store

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestProjectStatsAndCursors(t *testing.T) {
	st := tmp(t)
	if _, err := st.WriteClaim(rec("A1", "decision", "We decided to use jose, not jsonwebtoken, for Edge.", []string{"src/a.ts"})); err != nil {
		t.Fatal(err)
	}
	if _, err := st.WriteClaim(rec("A2", "failed", "Redis token bucket failed in src/a.ts staging.", []string{"src/a.ts"})); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(t.TempDir(), "chat.jsonl")
	if err := os.WriteFile(src, []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := st.SetCursor(src, 3); err != nil {
		t.Fatal(err)
	}
	if err := st.UpsertSession(Session{JSONL: src, SessionID: "s1", Harness: "grok", Project: "acme/api"}); err != nil {
		t.Fatal(err)
	}
	raw := st.RawPath("acme/api", "s1", time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC))
	if err := os.MkdirAll(filepath.Dir(raw), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(raw, []byte("rawline\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	stats, err := st.ListProjectStats()
	if err != nil {
		t.Fatal(err)
	}
	if len(stats) != 1 || stats[0].Key != "acme/api" || stats[0].Active != 2 {
		t.Fatalf("%+v", stats)
	}
	if stats[0].ByType["decision"] != 1 || stats[0].ByType["failed"] != 1 {
		t.Fatalf("types %+v", stats[0].ByType)
	}
	if stats[0].Sessions != 1 || stats[0].RawFiles < 1 || stats[0].RawBytes < 1 {
		t.Fatalf("sess/raw %+v", stats[0])
	}

	by, err := st.CountByType("acme/api")
	if err != nil || by["failed"] != 1 {
		t.Fatalf("%v %v", by, err)
	}
	recent, err := st.ListRecentActive("acme/api", 1)
	if err != nil || len(recent) != 1 {
		t.Fatalf("%+v %v", recent, err)
	}

	curs, err := st.ListCursors()
	if err != nil || len(curs) != 1 || curs[0].Status != "behind" {
		t.Fatalf("%+v %v", curs, err)
	}
	if err := st.SetCursor(src, int64(len("hello\n"))); err != nil {
		t.Fatal(err)
	}
	curs, _ = st.ListCursors()
	if curs[0].Status != "ok" {
		t.Fatalf("ok %+v", curs)
	}
	if err := st.SetCursor(src, 99); err != nil {
		t.Fatal(err)
	}
	curs, _ = st.ListCursors()
	if curs[0].Status != "past-eof" {
		t.Fatalf("past-eof %+v", curs)
	}
}
