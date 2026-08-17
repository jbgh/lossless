package inspect

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"lossless/internal/claim"
	"lossless/internal/retrieve"
	"lossless/internal/store"
)

func TestBuildAndAskInspect(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if _, err := st.WriteClaim(claim.Record{
		ID: "01JJOSE", Type: "decision", ProjectKey: "acme/api",
		Text:  "We decided to use jose, not jsonwebtoken, for Edge in src/middleware/auth.ts.",
		Paths: []string{"src/middleware/auth.ts"}, CreatedAt: "2026-07-01T00:00:00Z",
		Harness: "grok", SessionID: "s1", Status: "active", Source: "import",
	}); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(t.TempDir(), "s.jsonl")
	if err := os.WriteFile(src, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := st.SetCursor(src, 2); err != nil {
		t.Fatal(err)
	}
	if err := st.UpsertSession(store.Session{JSONL: src, SessionID: "s1", Harness: "grok", Project: "acme/api"}); err != nil {
		t.Fatal(err)
	}

	rep, err := Build(st, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Projects) != 1 || rep.Projects[0].Active != 1 {
		t.Fatalf("%+v", rep.Projects)
	}
	var buf strings.Builder
	Format(&buf, rep)
	if !strings.Contains(buf.String(), "acme/api") || !strings.Contains(buf.String(), "records 1") {
		t.Fatal(buf.String())
	}

	det, err := Build(st, "acme/api")
	if err != nil || det.Detail == nil || len(det.Detail.Recent) != 1 {
		t.Fatalf("%+v %v", det.Detail, err)
	}

	now := time.Date(2026, 8, 16, 0, 0, 0, 0, time.UTC)
	ask, err := Ask(st, retrieve.Request{
		Project: "acme/api", Question: "why not jsonwebtoken",
		Paths: []string{"src/middleware/auth.ts"},
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(ask.Hits) == 0 || !strings.Contains(ask.Hits[0].Text, "jose") {
		t.Fatalf("%+v", ask)
	}
	if ask.Hits[0].Why == "" || ask.Hits[0].Path == 0 || !strings.Contains(ask.Hits[0].Why, "score=") {
		t.Fatalf("why %+v", ask.Hits[0])
	}

	if _, err := st.WriteClaim(claim.Record{
		ID: "01NOISE", Type: "state", ProjectKey: "acme/api",
		Text:  "Working on src/middleware/auth.ts next.",
		Paths: []string{"src/middleware/auth.ts"}, CreatedAt: "2026-08-01T00:00:00Z",
		Harness: "grok", SessionID: "s1", Status: "active", Source: "import",
	}); err != nil {
		t.Fatal(err)
	}
	det2, err := Build(st, "acme/api")
	if err != nil || det2.Detail == nil || det2.Detail.RecentNoise < 1 {
		t.Fatalf("noise %+v %v", det2.Detail, err)
	}
	var buf2 strings.Builder
	Format(&buf2, det2)
	if !strings.Contains(buf2.String(), "ask-would-drop") || !strings.Contains(buf2.String(), "noise") {
		t.Fatal(buf2.String())
	}

	ask2, err := Ask(st, retrieve.Request{
		Project: "acme/api", Question: "why not jsonwebtoken",
		Paths: []string{"src/middleware/auth.ts"},
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	foundNoise := false
	for _, d := range ask2.Dropped {
		if strings.Contains(d.Why, "extract-noise") && strings.Contains(d.Text, "Working on") {
			foundNoise = true
		}
	}
	if !foundNoise {
		t.Fatalf("expected extract-noise drop %+v", ask2.Dropped)
	}

	jsonl := filepath.Join(t.TempDir(), "sess.jsonl")
	line := `{"role":"assistant","content":"We decided to use jose, not jsonwebtoken, for Edge in src/middleware/auth.ts."}` + "\n"
	if err := os.WriteFile(jsonl, []byte(line), 0o644); err != nil {
		t.Fatal(err)
	}
	ex, err := ExtractFile(jsonl, "acme/api")
	if err != nil || ex.Kept < 1 {
		t.Fatalf("%+v %v", ex, err)
	}
}
