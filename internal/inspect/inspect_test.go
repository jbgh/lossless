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
	if _, err := ExtractFile("/etc/passwd", "acme/api"); err == nil {
		t.Fatal("passwd")
	}
}

func TestInspectRecentResidueIsNoise(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	recs := []claim.Record{
		{ID: "R2019", Type: "decision", Text: "Live export still has recap-faileds in the recent window, and the tests gold yesterday’s skip phrases instead of proving that window is obey-worthy.", CreatedAt: "2026-08-19T20:19:11Z"},
		{ID: "R2013", Type: "failed", Text: "Inspect recent on the live store still includes the recap failed “Live recent 8 are slice-loop…”, which the uncommitted 0.1.7 gates already skip (`a packed failed` / `inspect recent 8`).", CreatedAt: "2026-08-19T20:13:51Z"},
		{ID: "R2010", Type: "failed", Text: "Live recent 8 are slice-loop / 0.1.5 decisions and version state; `recent_noise=0`; 17:43 is not a packed failed.", CreatedAt: "2026-08-19T20:10:28Z"},
		{ID: "R1915", Type: "failed", Text: "e_test.go lock the recap row, not a pathful named-lock failed (contrast Redis token bucket).", CreatedAt: "2026-08-19T19:15:38Z"},
		{ID: "R1909", Type: "failed", Text: "tree: productCopy slogans not bare never; space-form Same failure twice Redis still extracts; 0.1.", CreatedAt: "2026-08-19T19:09:59Z"},
		{ID: "THEYREDIS", Type: "failed", Text: "They found Redis token bucket failed in src/middleware/auth.ts staging.", Paths: []string{"src/middleware/auth.ts"}, CreatedAt: "2026-08-19T20:18:47Z"},
		{ID: "REAL01", Type: "failed", Text: "Redis token bucket failed in src/middleware/auth.ts staging.", Paths: []string{"src/middleware/auth.ts"}, CreatedAt: "2026-08-19T18:51:54Z"},
		{ID: "NAMED", Type: "failed", Text: "Named locks in catchup.go stay on the session JSONL.", CreatedAt: "2026-08-19T18:40:00Z"},
	}
	for _, r := range recs {
		r.ProjectKey = "acme/api"
		r.Status = "active"
		r.Source = "import"
		r.Harness = "grok"
		r.SessionID = "s-live"
		if _, err := st.WriteClaim(r); err != nil {
			t.Fatal(err)
		}
	}
	det, err := Build(st, "acme/api")
	if err != nil || det.Detail == nil || det.Detail.RecentNoise != 5 {
		t.Fatalf("recent_noise want 5 (mixed 8) got %+v %v", det.Detail, err)
	}
	var inWindowRedis, inWindowNoise, inWindowKeep, saw2010, saw2013 int
	for _, c := range det.Detail.Recent {
		if strings.Contains(c.Text, "`recent_noise=0`") {
			saw2010++
			if !retrieve.ExtractNoise(c) {
				t.Fatal("20:10 recap not noise")
			}
		}
		if strings.Contains(c.Text, "Inspect recent on the live store") {
			saw2013++
			if !retrieve.ExtractNoise(c) {
				t.Fatal("20:13 recap not noise")
			}
		}
		if strings.Contains(c.Text, "token bucket failed in src/middleware/auth.ts staging") {
			inWindowRedis++
			if retrieve.ExtractNoise(c) {
				t.Fatal("redis counted as noise")
			}
			continue
		}
		if retrieve.ExtractNoise(c) {
			inWindowNoise++
			continue
		}
		inWindowKeep++
	}
	if saw2010 != 1 || saw2013 != 1 || inWindowRedis != 2 || inWindowNoise != 5 || inWindowKeep != 1 {
		t.Fatalf("mixed window 2010=%d 2013=%d redis=%d noise=%d keep=%d recent=%+v", saw2010, saw2013, inWindowRedis, inWindowNoise, inWindowKeep, det.Detail.Recent)
	}
	var buf strings.Builder
	Format(&buf, det)
	if !strings.Contains(buf.String(), "ask-would-drop") {
		t.Fatal(buf.String())
	}
	if !strings.Contains(buf.String(), "e_test.go") || !strings.Contains(buf.String(), "Inspect recent on") {
		t.Fatalf("format missing live recaps %s", buf.String())
	}
	now := time.Date(2026, 8, 19, 20, 30, 0, 0, time.UTC)
	ask, err := Ask(st, retrieve.Request{
		Project: "acme/api", Question: "what failed on auth",
		Goal:  "fix the limiter",
		Paths: []string{"src/middleware/auth.ts"},
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	packedRedis := false
	for _, h := range ask.Hits {
		if strings.Contains(h.Text, "still extracts") || strings.Contains(h.Text, "lock the recap row") ||
			strings.Contains(h.Text, "still store and pack") || strings.Contains(h.Text, "recap-as-failed") ||
			strings.Contains(h.Text, "control-flow holes") || strings.Contains(h.Text, "recap-like") ||
			strings.Contains(h.Text, "recent_noise=0") || strings.Contains(h.Text, "Inspect recent on the live store") {
			t.Fatalf("residue packed: %+v", ask.Hits)
		}
		if strings.Contains(h.Text, "token bucket failed in src/middleware/auth.ts staging") {
			packedRedis = true
		}
	}
	if !packedRedis {
		t.Fatalf("Redis failed missed: %+v", ask.Hits)
	}
	droppedRecap := false
	for _, d := range ask.Dropped {
		if !strings.Contains(d.Why, "extract-noise") {
			continue
		}
		if strings.Contains(d.Text, "still extracts") || strings.Contains(d.Text, "lock the recap row") ||
			strings.Contains(d.Text, "recent_noise=0") || strings.Contains(d.Text, "Inspect recent on the live store") ||
			strings.Contains(d.Text, "recap-faileds") {
			droppedRecap = true
		}
	}
	if !droppedRecap {
		t.Fatalf("expected extract-noise drop of a live recap %+v", ask.Dropped)
	}
}

func TestInspectRecentExampleDropIsNoise(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	recs := []claim.Record{
		{ID: "EXAMPLEDROP", Type: "failed", Text: "Example drop: They found Redis token bucket failed in src/middleware/auth.ts staging.", Paths: []string{"src/middleware/auth.ts"}, CreatedAt: "2026-08-19T20:18:47Z"},
		{ID: "STILLSTORES", Type: "failed", Text: "A They-found Redis/path failed still stores.", CreatedAt: "2026-08-19T20:59:56Z"},
		{ID: "THEYREDIS", Type: "failed", Text: "They found Redis token bucket failed in src/middleware/auth.ts staging.", Paths: []string{"src/middleware/auth.ts"}, CreatedAt: "2026-08-19T20:18:46Z"},
		{ID: "REDISOK", Type: "failed", Text: "Redis token bucket failed in src/middleware/auth.ts staging.", Paths: []string{"src/middleware/auth.ts"}, CreatedAt: "2026-08-19T18:51:54Z"},
	}
	for _, r := range recs {
		r.ProjectKey = "acme/api"
		r.Status = "active"
		r.Source = "import"
		r.Harness = "grok"
		r.SessionID = "s-live"
		if _, err := st.WriteClaim(r); err != nil {
			t.Fatal(err)
		}
	}
	det, err := Build(st, "acme/api")
	if err != nil || det.Detail == nil || det.Detail.RecentNoise != 2 {
		t.Fatalf("recent_noise want 2 got %+v %v", det.Detail, err)
	}
	var noiseDrop, goldKeep int
	for _, c := range det.Detail.Recent {
		if strings.Contains(c.Text, "Example drop:") || strings.Contains(c.Text, "still stores.") {
			if !retrieve.ExtractNoise(c) {
				t.Fatalf("recap not noise: %q", c.Text)
			}
			noiseDrop++
			continue
		}
		if strings.Contains(c.Text, "token bucket failed in src/middleware/auth.ts staging") {
			if retrieve.ExtractNoise(c) {
				t.Fatalf("gold Redis as noise: %q", c.Text)
			}
			goldKeep++
		}
	}
	if noiseDrop != 2 || goldKeep != 2 {
		t.Fatalf("example-drop window noise=%d gold=%d recent=%+v", noiseDrop, goldKeep, det.Detail.Recent)
	}
}

func TestInspectRecent019RecapsAreNoise(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	recs := []claim.Record{
		{ID: "R2208", Type: "failed", Text: "Bench failed against current testdata.", CreatedAt: "2026-08-19T22:08:14Z"},
		{ID: "R2157", Type: "failed", Text: "Shared `SkipProse` / `ExtractNoise` now drop the five live inspect-recap shapes that were impersonating work, so a later session or another tool is less likely to be handed a fake Redis failed.", CreatedAt: "2026-08-19T21:57:10Z"},
		{ID: "R2146", Type: "decision", Text: "go-first, 4.0, 0.1.3 remember, Authorization, I'll stick with JWT next.", CreatedAt: "2026-08-19T21:46:01Z"},
		{ID: "R2149", Type: "decision", Text: "Secrets path unchanged; tests copy recap bodies instead of reading ~/.", CreatedAt: "2026-08-19T21:49:37Z"},
		{ID: "R2049", Type: "failed", Text: "The gates mostly match the listed shapes, and extract does keep the gold They-found Redis failed plus real named-lock faileds.", CreatedAt: "2026-08-19T20:49:15Z"},
		{ID: "REDISOK", Type: "failed", Text: "Redis token bucket failed in src/middleware/auth.ts staging.", Paths: []string{"src/middleware/auth.ts"}, CreatedAt: "2026-08-19T18:51:54Z"},
		{ID: "JWTNEXT", Type: "decision", Text: "I'll stick with JWT next.", CreatedAt: "2026-08-19T21:40:00Z"},
		{ID: "VERKEEP", Type: "constraint", Text: "version.go and CHANGELOG must match.", CreatedAt: "2026-08-19T22:01:35Z"},
	}
	for _, r := range recs {
		r.ProjectKey = "acme/api"
		r.Status = "active"
		r.Source = "import"
		r.Harness = "grok"
		r.SessionID = "s-live"
		if _, err := st.WriteClaim(r); err != nil {
			t.Fatal(err)
		}
	}
	det, err := Build(st, "acme/api")
	if err != nil || det.Detail == nil || det.Detail.RecentNoise != 5 {
		t.Fatalf("recent_noise want 5 got %+v %v", det.Detail, err)
	}
	var noiseDrop, goldKeep int
	for _, c := range det.Detail.Recent {
		switch {
		case strings.Contains(c.Text, "current testdata"), strings.Contains(c.Text, "inspect-recap"),
			strings.Contains(c.Text, "go-first"), strings.Contains(c.Text, "recap bodies"),
			strings.Contains(c.Text, "gates mostly match"):
			if !retrieve.ExtractNoise(c) {
				t.Fatalf("recap not noise: %q", c.Text)
			}
			noiseDrop++
		case strings.Contains(c.Text, "token bucket failed in src/middleware/auth.ts staging"),
			c.Text == "I'll stick with JWT next.",
			c.Text == "version.go and CHANGELOG must match.":
			if retrieve.ExtractNoise(c) {
				t.Fatalf("keep as noise: %q", c.Text)
			}
			goldKeep++
		}
	}
	if noiseDrop != 5 || goldKeep != 3 {
		t.Fatalf("0.1.9 window noise=%d gold=%d recent=%+v", noiseDrop, goldKeep, det.Detail.Recent)
	}
}

func TestInspectRecentPostPruneRecapsAreNoise(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	recs := []claim.Record{
		{ID: "R2237", Type: "failed", Text: "Gold Redis/named-lock/JWT/Tests-failed-to/concurrent_test.", CreatedAt: "2026-08-19T22:37:54Z"},
		{ID: "R2232", Type: "failed", Text: "Locks still keep They-found Redis/path, named-lock, JWT next, Tests failed to, concurrent_test.", CreatedAt: "2026-08-19T22:32:42Z"},
		{ID: "R2227COLON", Type: "failed", Text: "Locks still store: They-found Redis/path, named-lock, JWT next, Tests failed to connect, concurrent_test.", CreatedAt: "2026-08-19T22:27:44Z"},
		{ID: "R2227JUDGE", Type: "decision", Text: "Recent 8 is not all obey-worthy (`A later session still checks out recap instead of work` plus slice-loop judge residue), so 0.3 stays open.", CreatedAt: "2026-08-19T22:27:45Z"},
		{ID: "R2217", Type: "decision", Text: "A later session still checks out recap instead of work.", CreatedAt: "2026-08-19T22:17:27Z"},
		{ID: "REDISOK", Type: "failed", Text: "Redis token bucket failed in src/middleware/auth.ts staging.", Paths: []string{"src/middleware/auth.ts"}, CreatedAt: "2026-08-19T22:30:00Z"},
		{ID: "JWTNEXT", Type: "decision", Text: "I'll stick with JWT next.", CreatedAt: "2026-08-19T22:29:00Z"},
		{ID: "VERKEEP", Type: "constraint", Text: "version.go and CHANGELOG must match.", CreatedAt: "2026-08-19T22:28:00Z"},
	}
	for _, r := range recs {
		r.ProjectKey = "acme/api"
		r.Status = "active"
		r.Source = "import"
		r.Harness = "grok"
		r.SessionID = "s-live"
		if _, err := st.WriteClaim(r); err != nil {
			t.Fatal(err)
		}
	}
	det, err := Build(st, "acme/api")
	if err != nil || det.Detail == nil || det.Detail.RecentNoise != 5 {
		t.Fatalf("recent_noise want 5 got %+v %v", det.Detail, err)
	}
	var noiseDrop, goldKeep int
	for _, c := range det.Detail.Recent {
		switch {
		case strings.Contains(c.Text, "Tests-failed-to"), strings.Contains(c.Text, "still keep They-found"),
			strings.Contains(c.Text, "still store:"), strings.Contains(c.Text, "obey-worthy"),
			strings.Contains(c.Text, "checks out recap") && !strings.Contains(c.Text, "obey-worthy"):
			if !retrieve.ExtractNoise(c) {
				t.Fatalf("recap not noise: %q", c.Text)
			}
			noiseDrop++
		case strings.Contains(c.Text, "token bucket failed in src/middleware/auth.ts staging"),
			c.Text == "I'll stick with JWT next.",
			c.Text == "version.go and CHANGELOG must match.":
			if retrieve.ExtractNoise(c) {
				t.Fatalf("keep as noise: %q", c.Text)
			}
			goldKeep++
		}
	}
	if noiseDrop != 5 || goldKeep != 3 {
		t.Fatalf("post-prune window noise=%d gold=%d recent=%+v", noiseDrop, goldKeep, det.Detail.Recent)
	}
}

func TestInspectRecentHyphenLockListIsNoise(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	recs := []claim.Record{
		{ID: "R2256", Type: "failed", Text: "They-found Redis, named-lock, JWT next, Tests failed to, version.go, and 4.0 still keep.", CreatedAt: "2026-08-19T22:56:27Z"},
		{ID: "R2255", Type: "decision", Text: "Colon-form still-store lock-list recaps skip. A concurrent_test.go-first failed is not a go-first mash.", CreatedAt: "2026-08-19T22:55:52Z", Paths: []string{"internal/gate/gate.go", "internal/write/extract.go"}},
		{ID: "R2255ALONE", Type: "decision", Text: "Colon-form still-store lock-list recaps skip.", CreatedAt: "2026-08-19T22:55:00Z"},
		{ID: "RGROUND", Type: "failed", Text: "Pathful Bench and Failed to still ground.", CreatedAt: "2026-08-19T22:54:00Z"},
		{ID: "REDISOK", Type: "failed", Text: "Redis token bucket failed in src/middleware/auth.ts staging.", Paths: []string{"src/middleware/auth.ts"}, CreatedAt: "2026-08-19T22:53:00Z"},
		{ID: "JWTNEXT", Type: "decision", Text: "I'll stick with JWT next.", CreatedAt: "2026-08-19T22:52:00Z"},
		{ID: "VERKEEP", Type: "constraint", Text: "version.go and CHANGELOG must match.", CreatedAt: "2026-08-19T22:51:00Z"},
		{ID: "TESTGO", Type: "failed", Text: "concurrent_test.go-first File locks failed to acquire.", CreatedAt: "2026-08-19T22:50:00Z"},
	}
	for _, r := range recs {
		r.ProjectKey = "acme/api"
		r.Status = "active"
		r.Source = "import"
		r.Harness = "grok"
		r.SessionID = "s-live"
		if _, err := st.WriteClaim(r); err != nil {
			t.Fatal(err)
		}
	}
	det, err := Build(st, "acme/api")
	if err != nil || det.Detail == nil || det.Detail.RecentNoise != 4 {
		t.Fatalf("recent_noise want 4 got %+v %v", det.Detail, err)
	}
	var noiseDrop, goldKeep int
	for _, c := range det.Detail.Recent {
		switch {
		case strings.Contains(c.Text, "and 4.0 still keep"), strings.Contains(c.Text, "lock-list recaps skip"),
			strings.Contains(c.Text, "still ground"):
			if !retrieve.ExtractNoise(c) {
				t.Fatalf("recap not noise: %q", c.Text)
			}
			noiseDrop++
		case strings.Contains(c.Text, "token bucket failed in src/middleware/auth.ts staging"),
			c.Text == "I'll stick with JWT next.",
			c.Text == "version.go and CHANGELOG must match.",
			c.Text == "concurrent_test.go-first File locks failed to acquire.":
			if retrieve.ExtractNoise(c) {
				t.Fatalf("keep as noise: %q", c.Text)
			}
			goldKeep++
		}
	}
	if noiseDrop != 4 || goldKeep != 4 {
		t.Fatalf("hyphen lock-list window noise=%d gold=%d recent=%+v", noiseDrop, goldKeep, det.Detail.Recent)
	}
}
