package eval

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"lossless/internal/claim"
	"lossless/internal/retrieve"
	"lossless/internal/store"
)

type fixture struct {
	ID                       string           `json:"id"`
	EmptyStore               bool             `json:"empty_store"`
	Request                  retrieve.Request `json:"request"`
	MustIncludeTypes         []string         `json:"must_include_types"`
	MustIncludeSubstrings    []string         `json:"must_include_substrings"`
	MustNotIncludeSubstrings []string         `json:"must_not_include_substrings"`
	MustIncludeIDs           []string         `json:"must_include_ids"`
	MustWarn                 string           `json:"must_warn"`
	MustNotWarn              bool             `json:"must_not_warn"`
	MustBeEmpty              bool             `json:"must_be_empty"`
	TopTypes                 []string         `json:"top_types"`
	FirstMustNotContain      string           `json:"first_must_not_contain"`
	MinHits                  int              `json:"min_hits"`
	MaxTokens                int              `json:"max_tokens"`
}

func seed(t *testing.T, empty bool) *store.Store {
	t.Helper()
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if empty {
		return st
	}
	recs := []claim.Record{
		{ID: "01JFAIL", Type: "failed", Text: "Redis token bucket failed in staging; connection pool exhausted.", Paths: []string{"src/middleware/auth.ts"}, CreatedAt: "2026-08-01T18:12:00Z"},
		{ID: "01JJOSE", Type: "decision", Text: "Use jose, not jsonwebtoken, for Edge.", Paths: []string{"src/middleware/auth.ts"}, CreatedAt: "2026-07-20T11:00:00Z"},
		{ID: "01JRATE", Type: "decision", Text: "Rate limiter lives in src/middleware/auth.ts as an in-process token bucket.", Paths: []string{"src/middleware/auth.ts"}, CreatedAt: "2026-07-22T09:00:00Z"},
		{ID: "01JAUTHZ", Type: "constraint", Text: "Never log Authorization headers.", Paths: []string{"src/middleware/auth.ts"}, CreatedAt: "2026-06-01T00:00:00Z"},
		{ID: "01JBILL", Type: "state", Text: "Working on billing invoices export, not auth.", Paths: []string{"src/billing/export.ts"}, CreatedAt: "2026-08-10T00:00:00Z"},
	}
	for _, rec := range recs {
		rec.ProjectKey = "acme/api"
		rec.Harness = "grok"
		rec.SessionID = "eval"
		rec.Source = "import"
		rec.Status = "active"
		if _, err := st.WriteClaim(rec); err != nil {
			t.Fatal(err)
		}
	}
	return st
}

func TestAskFixtures(t *testing.T) {
	files, err := filepath.Glob("ask/*.json")
	if err != nil || len(files) == 0 {
		t.Fatal(err, files)
	}
	engNow := func() time.Time { return time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC) }
	for _, f := range files {
		f := f
		t.Run(filepath.Base(f), func(t *testing.T) {
			b, err := os.ReadFile(f)
			if err != nil {
				t.Fatal(err)
			}
			var fx fixture
			if err := json.Unmarshal(b, &fx); err != nil {
				t.Fatal(err)
			}
			st := seed(t, fx.EmptyStore)
			out, err := (retrieve.Engine{Store: st, Now: engNow}).Ask(fx.Request)
			if err != nil {
				t.Fatal(err)
			}
			if fx.MustBeEmpty {
				if len(out.Context) != 0 || len(out.Warnings) != 0 {
					t.Fatalf("%+v", out)
				}
				return
			}
			joined := ""
			types := map[string]bool{}
			ids := map[string]bool{}
			for _, h := range out.Context {
				joined += h.Text + "\n"
				types[h.Type] = true
				ids[h.ID] = true
			}
			for _, typ := range fx.MustIncludeTypes {
				if !types[typ] {
					t.Fatalf("missing type %s in %+v", typ, out.Context)
				}
			}
			for _, s := range fx.MustIncludeSubstrings {
				if !strings.Contains(joined, s) {
					t.Fatalf("missing %q in %q", s, joined)
				}
			}
			for _, s := range fx.MustNotIncludeSubstrings {
				if strings.Contains(joined, s) {
					t.Fatalf("unexpected %q in %q", s, joined)
				}
			}
			for _, id := range fx.MustIncludeIDs {
				if !ids[id] {
					t.Fatalf("missing id %s", id)
				}
			}
			if fx.MustWarn != "" {
				ok := false
				for _, w := range out.Warnings {
					if strings.Contains(strings.ToLower(w), fx.MustWarn) {
						ok = true
					}
				}
				if !ok {
					t.Fatalf("want warn %q got %v", fx.MustWarn, out.Warnings)
				}
			}
			if fx.MustNotWarn && len(out.Warnings) != 0 {
				t.Fatalf("warn %v", out.Warnings)
			}
			if len(fx.TopTypes) > 0 && len(out.Context) > 0 {
				ok := false
				for _, typ := range fx.TopTypes {
					if out.Context[0].Type == typ {
						ok = true
					}
				}
				if !ok {
					t.Fatalf("top type %s", out.Context[0].Type)
				}
			}
			if fx.FirstMustNotContain != "" && len(out.Context) > 0 && strings.Contains(out.Context[0].Text, fx.FirstMustNotContain) {
				t.Fatalf("first hit %q", out.Context[0].Text)
			}
			if fx.MinHits > 0 && len(out.Context) < fx.MinHits {
				t.Fatalf("hits %d", len(out.Context))
			}
			if fx.MaxTokens > 0 && out.Tokens > fx.MaxTokens {
				t.Fatalf("tokens %d", out.Tokens)
			}
		})
	}
}

func TestStaleVerifyFixture(t *testing.T) {
	st := seed(t, true)
	ws := t.TempDir()
	if err := os.MkdirAll(filepath.Join(ws, "src/middleware"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws, "src/middleware/auth.ts"), []byte("new\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := st.WriteClaim(claim.Record{
		ID: "01JLIM", Type: "decision", ProjectKey: "acme/api",
		Text: "Keep the in-process limiter in src/middleware/auth.ts",
		Paths: []string{"src/middleware/auth.ts"},
		PathMtime: map[string]int64{"src/middleware/auth.ts": 1},
		CreatedAt: "2026-08-01T00:00:00Z", Harness: "grok", SessionID: "e", Source: "import", Status: "active",
	}); err != nil {
		t.Fatal(err)
	}
	out, err := (retrieve.Engine{Store: st, Now: func() time.Time {
		return time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)
	}}).Ask(retrieve.Request{
		Project: "acme/api", WorkspaceRoot: ws, Goal: "change auth limiter",
		Paths: []string{"src/middleware/auth.ts"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Context) == 0 || !strings.HasPrefix(out.Context[0].Text, "[verify]") {
		t.Fatalf("%+v", out)
	}
	got, _ := st.Get("01JLIM")
	if strings.HasPrefix(got.Text, "[verify]") {
		t.Fatal("persisted")
	}
}

func TestDiversityAndSupersedeFixtures(t *testing.T) {
	st := seed(t, true)
	for _, rec := range []claim.Record{
		{ID: "01JA", Type: "decision", Text: "Use jose, not jsonwebtoken, for Edge.", CreatedAt: "2026-07-20T11:00:00Z"},
		{ID: "01JB", Type: "decision", Text: "Use jose not jsonwebtoken for Edge runtime.", CreatedAt: "2026-07-21T11:00:00Z"},
	} {
		rec.ProjectKey = "acme/api"
		rec.Harness = "grok"
		rec.SessionID = "e"
		rec.Source = "import"
		rec.Status = "active"
		if _, err := st.WriteClaim(rec); err != nil {
			t.Fatal(err)
		}
	}
	out, err := retrieve.Ask(st, retrieve.Request{Project: "acme/api", Question: "why not jsonwebtoken"})
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, h := range out.Context {
		if strings.Contains(h.Text, "jose") {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("diversity packed %d: %+v", n, out.Context)
	}

	st2 := seed(t, true)
	old := claim.Record{ID: "OLD", Type: "decision", Text: "Use jose, not jsonwebtoken, for Edge.", ProjectKey: "acme/api", Harness: "grok", SessionID: "e", Source: "import", Status: "active", CreatedAt: "2026-07-01T00:00:00Z"}
	if _, err := st2.WriteClaim(old); err != nil {
		t.Fatal(err)
	}
	if _, err := st2.WriteClaim(claim.Record{ID: "NEW", Type: "decision", Text: "Use jose, not jsonwebtoken, for Edge.", ProjectKey: "acme/api", Harness: "grok", SessionID: "e", Source: "import", Status: "active", CreatedAt: "2026-08-01T00:00:00Z"}); err != nil {
		t.Fatal(err)
	}
	out, err = retrieve.Ask(st2, retrieve.Request{Project: "acme/api", Question: "why not jsonwebtoken"})
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range out.Context {
		if h.ID == "OLD" {
			t.Fatal("superseded leaked")
		}
	}
}

func TestSecretNotRetrieved(t *testing.T) {
	st := seed(t, false)
	out, err := retrieve.Ask(st, retrieve.Request{Project: "acme/api", Question: "AKIAIOSFODNN7EXAMPLE"})
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range out.Context {
		if strings.Contains(h.Text, "AKIA") {
			t.Fatal(h.Text)
		}
	}
}
