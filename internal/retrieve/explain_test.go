package retrieve

import (
	"strings"
	"testing"

	"lossless/internal/claim"
	"lossless/internal/store"
)

func TestExplainMatchesAskAndReportsNoise(t *testing.T) {
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
	if _, err := st.WriteClaim(claim.Record{
		ID: "01NOISE", Type: "state", ProjectKey: "acme/api",
		Text:  "Working on src/middleware/auth.ts next.",
		Paths: []string{"src/middleware/auth.ts"}, CreatedAt: "2026-08-01T00:00:00Z",
		Harness: "grok", SessionID: "s1", Status: "active", Source: "import",
	}); err != nil {
		t.Fatal(err)
	}
	req := Request{
		Project: "acme/api", Question: "why not jsonwebtoken",
		Paths: []string{"src/middleware/auth.ts"},
	}
	out, err := Ask(st, req)
	if err != nil {
		t.Fatal(err)
	}
	tr, err := Explain(st, req)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Context) != len(tr.Packed) {
		t.Fatalf("packed %d vs ask %d", len(tr.Packed), len(out.Context))
	}
	for i, h := range out.Context {
		if h.ID != tr.Packed[i].ID {
			t.Fatalf("id %s vs %s", h.ID, tr.Packed[i].ID)
		}
	}
	found := false
	for _, d := range tr.Dropped {
		if d.ID == "01NOISE" && strings.Contains(d.Why, "extract-noise") {
			found = true
		}
	}
	if !found {
		t.Fatalf("dropped %+v", tr.Dropped)
	}
	if len(tr.Packed) == 0 || tr.Packed[0].Why == "" {
		t.Fatalf("packed why %+v", tr.Packed)
	}
}
