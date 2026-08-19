package inspect

import (
	"os"
	"path/filepath"
	"testing"

	"lossless/internal/claim"
	"lossless/internal/store"
)

func TestPruneDropsTestIngestKeepsLive(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	ws := "/var/folders/xx/T/TestRunHookGrok123456789/003"
	src := filepath.Join(t.TempDir(), "chat.jsonl")
	if err := os.WriteFile(src, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := st.WriteClaim(claim.Record{
		ID: "TESTJOSE", Type: "decision", ProjectKey: "path-deadbeefdeadbee",
		Text:      "We decided to use jose, not jsonwebtoken, for Edge.",
		CreatedAt: "2026-08-01T00:00:00Z", SessionID: "sess1", Status: "active",
		Source: "import", Harness: "grok",
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.UpsertSession(store.Session{
		JSONL: src, SessionID: "sess1", Harness: "grok",
		Workspace: ws, Project: "path-deadbeefdeadbee",
	}); err != nil {
		t.Fatal(err)
	}

	if _, err := st.WriteClaim(claim.Record{
		ID: "LIVEJOSE", Type: "decision", ProjectKey: "acme/api",
		Text:  "We decided to use jose, not jsonwebtoken, for Edge in src/middleware/auth.ts.",
		Paths: []string{"src/middleware/auth.ts"}, CreatedAt: "2026-07-01T00:00:00Z",
		SessionID: "01a003db-f4a6-7f43-a694-082428bbff32", Status: "active",
		Source: "import", Harness: "grok",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.WriteClaim(claim.Record{
		ID: "NOISE", Type: "state", ProjectKey: "acme/api",
		Text:  "This is still not a guarantee — a model can ignore a skill — but it is no longer one sentence on a tool next to grep.",
		Paths: []string{".claude/skills/lossless/SKILL.md"}, CreatedAt: "2026-08-17T04:41:01Z",
		SessionID: "01a003db-f4a6-7f43-a694-082428bbff32", Status: "active",
		Source: "import", Harness: "grok",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.WriteClaim(claim.Record{
		ID: "FIX", Type: "decision", ProjectKey: "acme/api",
		Text: "We decided to use jose from a fixture.", SessionID: "grok-auth",
		CreatedAt: "2026-08-01T00:00:00Z", Status: "active", Source: "import", Harness: "grok",
	}); err != nil {
		t.Fatal(err)
	}

	res, err := Prune(st, "")
	if err != nil {
		t.Fatal(err)
	}
	if res.DroppedRecords < 1 || res.SupersededNoise < 1 {
		t.Fatalf("%+v", res)
	}
	if _, ok := st.Get("TESTJOSE"); ok {
		t.Fatal("test ingest remained")
	}
	if _, ok := st.Get("FIX"); ok {
		t.Fatal("fixture claim remained")
	}
	live, ok := st.Get("LIVEJOSE")
	if !ok || live.Status != "active" {
		t.Fatal("live claim")
	}
	noise, ok := st.Get("NOISE")
	if !ok || noise.Status != "superseded" {
		t.Fatalf("noise %+v %v", noise, ok)
	}
}

func TestPruneSupersedesSloganAndIntendedGap(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	for _, r := range []claim.Record{
		{
			ID: "SLOGAN", Type: "constraint", ProjectKey: "acme/api",
			Text:      "You can switch between them and never lose memory.",
			CreatedAt: "2026-08-19T16:40:35Z", SessionID: "s-live", Status: "active",
			Source: "import", Harness: "grok",
		},
		{
			ID: "GAP015", Type: "constraint", ProjectKey: "acme/api",
			Text:      "Intended gap: Shipped channel is still 0.1.5: OpenCode plugin-miss never reach the tape.",
			CreatedAt: "2026-08-19T16:40:34Z", SessionID: "s-live", Status: "active",
			Source: "import", Harness: "grok",
		},
		{
			ID: "HYPHEN", Type: "failed", ProjectKey: "acme/api",
			Text:      "0.1.7 extract-clean tree: slogans, I'll-run, intended-gap, same-failure-twice, process-state gated; Redis faileds and stick-with kept.",
			CreatedAt: "2026-08-19T17:36:52Z", SessionID: "s-live", Status: "active",
			Source: "import", Harness: "grok",
		},
		{
			ID: "TESTS", Type: "failed", ProjectKey: "acme/api",
			Text:      "Tests failed on the live residue as expected.",
			CreatedAt: "2026-08-19T17:26:54Z", SessionID: "s-live", Status: "active",
			Source: "import", Harness: "grok",
		},
		{
			ID: "JSONDUMP", Type: "failed", ProjectKey: "acme/api",
			Text:      `{"diff_stat":"13 files changed, 322 insertions(+), 6 deletions(-)","ok":true,"summary":"Gate SkipProse now treats hyphenated I'll-run."}`,
			CreatedAt: "2026-08-19T17:58:45Z", SessionID: "s-live", Status: "active",
			Source: "import", Harness: "grok",
		},
		{
			ID: "R1909", Type: "failed", ProjectKey: "acme/api",
			Text:      "tree: productCopy slogans not bare never; space-form Same failure twice Redis still extracts; 0.1.",
			CreatedAt: "2026-08-19T19:09:59Z", SessionID: "s-live", Status: "active",
			Source: "import", Harness: "grok",
		},
		{
			ID: "R1915", Type: "failed", ProjectKey: "acme/api",
			Text:      "e_test.go lock the recap row, not a pathful named-lock failed (contrast Redis token bucket).",
			CreatedAt: "2026-08-19T19:15:38Z", SessionID: "s-live", Status: "active",
			Source: "import", Harness: "grok",
		},
		{
			ID: "R1903", Type: "failed", ProjectKey: "acme/api",
			Text:      "Redis faileds, stick-with decisions, space-form “same failure twice” job-1, and pathless `Tests failed to` still store and pack.",
			CreatedAt: "2026-08-19T19:03:32Z", SessionID: "s-live", Status: "active",
			Source: "import", Harness: "grok",
		},
		{
			ID: "R1922", Type: "failed", ProjectKey: "acme/api",
			Text:      "Shipping the current tree would lock fail-close skips and recap-as-failed.",
			CreatedAt: "2026-08-19T19:22:10Z", SessionID: "s-live", Status: "active",
			Source: "import", Harness: "grok",
		},
		{
			ID: "R1743", Type: "failed", ProjectKey: "acme/api",
			Text:      "They found real control-flow holes: budget headroom too small, a failed semver check aborting the whole batch, and a “shipped N” summary when the run just ran out of slots.",
			CreatedAt: "2026-08-19T17:43:55Z", SessionID: "s-live", Status: "active",
			Source: "import", Harness: "grok",
		},
		{
			ID: "R1957", Type: "failed", ProjectKey: "acme/api",
			Text:      "One remaining active failed looks recap-like.",
			CreatedAt: "2026-08-19T19:57:05Z", SessionID: "s-live", Status: "active",
			Source: "import", Harness: "grok",
		},
		{
			ID: "R2010", Type: "failed", ProjectKey: "acme/api",
			Text:      "Live recent 8 are slice-loop / 0.1.5 decisions and version state; `recent_noise=0`; 17:43 is not a packed failed.",
			CreatedAt: "2026-08-19T20:10:28Z", SessionID: "s-live", Status: "active",
			Source: "import", Harness: "grok",
		},
		{
			ID: "R2013", Type: "failed", ProjectKey: "acme/api",
			Text:      "Inspect recent on the live store still includes the recap failed “Live recent 8 are slice-loop…”, which the uncommitted 0.1.7 gates already skip (`a packed failed` / `inspect recent 8`).",
			CreatedAt: "2026-08-19T20:13:51Z", SessionID: "s-live", Status: "active",
			Source: "import", Harness: "grok",
		},
		{
			ID: "R2019", Type: "decision", ProjectKey: "acme/api",
			Text:      "Live export still has recap-faileds in the recent window, and the tests gold yesterday’s skip phrases instead of proving that window is obey-worthy.",
			CreatedAt: "2026-08-19T20:19:11Z", SessionID: "s-live", Status: "active",
			Source: "import", Harness: "grok",
		},
		{
			ID: "THEYREDIS", Type: "failed", ProjectKey: "acme/api",
			Text:  "They found Redis token bucket failed in src/middleware/auth.ts staging.",
			Paths: []string{"src/middleware/auth.ts"}, CreatedAt: "2026-08-19T20:18:47Z",
			SessionID: "s-live", Status: "active", Source: "import", Harness: "grok",
		},
		{
			ID: "PROCRECAP", Type: "state", ProjectKey: "acme/api",
			Text:      "ProcessState is in SkipProse as planned; Working on … next is no longer a required kept state.",
			CreatedAt: "2026-08-19T18:47:17Z", SessionID: "s-live", Status: "active",
			Source: "import", Harness: "grok",
		},
		{
			ID: "REDISOK", Type: "failed", ProjectKey: "acme/api",
			Text:  "Redis token bucket failed in src/middleware/auth.ts staging.",
			Paths: []string{"src/middleware/auth.ts"}, CreatedAt: "2026-08-17T18:51:54Z",
			SessionID: "s-live", Status: "active", Source: "import", Harness: "grok",
		},
		{
			ID: "SAMEJOB1", Type: "failed", ProjectKey: "acme/api",
			Text:  "Same failure twice: Redis token bucket still 429 in src/middleware/auth.ts.",
			Paths: []string{"src/middleware/auth.ts"}, CreatedAt: "2026-08-19T18:05:00Z",
			SessionID: "s-live", Status: "active", Source: "import", Harness: "grok",
		},
		{
			ID: "NAMEDKEEP", Type: "failed", ProjectKey: "acme/api",
			Text:      "Named locks in catchup.go stay on the session JSONL.",
			CreatedAt: "2026-08-19T18:40:00Z", SessionID: "s-live", Status: "active",
			Source: "import", Harness: "grok",
		},
		{
			ID: "FILEKEEP", Type: "failed", ProjectKey: "acme/api",
			Text:      "File locks are tested in concurrent_test.go.",
			CreatedAt: "2026-08-19T18:39:00Z", SessionID: "s-live", Status: "active",
			Source: "import", Harness: "grok",
		},
		{
			ID: "TESTGOKEEP", Type: "failed", ProjectKey: "acme/api",
			Text:      "concurrent_test.go File locks failed to acquire.",
			CreatedAt: "2026-08-19T18:38:00Z", SessionID: "s-live", Status: "active",
			Source: "import", Harness: "grok",
		},
		{
			ID: "STILLSTORES", Type: "failed", ProjectKey: "acme/api",
			Text:      "A They-found Redis/path failed still stores.",
			CreatedAt: "2026-08-19T20:59:56Z", SessionID: "s-live", Status: "active",
			Source: "import", Harness: "grok",
		},
		{
			ID: "LOOPRESIDUE", Type: "failed", ProjectKey: "acme/api",
			Text:      "Those recaps are loop residue; the product keep is: a real They-found Redis/path failed still stores.",
			CreatedAt: "2026-08-19T20:39:27Z", SessionID: "s-live", Status: "active",
			Source: "import", Harness: "grok",
		},
		{
			ID: "EXAMPLEDROP", Type: "failed", ProjectKey: "acme/api",
			Text:  "Example drop: They found Redis token bucket failed in src/middleware/auth.ts staging.",
			Paths: []string{"src/middleware/auth.ts"}, CreatedAt: "2026-08-19T20:18:47Z",
			SessionID: "s-live", Status: "active", Source: "import", Harness: "grok",
		},
		{
			ID: "NEVERLOSEMEMO", Type: "constraint", ProjectKey: "acme/api",
			Text:      "Cross-harness, switch models, never lose memo",
			CreatedAt: "2026-08-19T20:39:27Z", SessionID: "s-live", Status: "active",
			Source: "import", Harness: "grok",
		},
		{
			ID: "UNCLOSEDPAREN", Type: "constraint", ProjectKey: "acme/api",
			Text:      "Non-empty must not prune other projects (memora etc.",
			CreatedAt: "2026-08-19T20:39:53Z", SessionID: "s-live", Status: "active",
			Source: "import", Harness: "grok",
		},
	} {
		if _, err := st.WriteClaim(r); err != nil {
			t.Fatal(err)
		}
	}
	res, err := Prune(st, "")
	if err != nil {
		t.Fatal(err)
	}
	if res.SupersededNoise < 18 {
		t.Fatalf("%+v", res)
	}
	slogan, ok := st.Get("SLOGAN")
	if !ok || slogan.Status != "superseded" {
		t.Fatalf("slogan %+v %v", slogan, ok)
	}
	gap, ok := st.Get("GAP015")
	if !ok || gap.Status != "superseded" {
		t.Fatalf("gap %+v %v", gap, ok)
	}
	hyphen, ok := st.Get("HYPHEN")
	if !ok || hyphen.Status != "superseded" {
		t.Fatalf("hyphen %+v %v", hyphen, ok)
	}
	tests, ok := st.Get("TESTS")
	if !ok || tests.Status != "superseded" {
		t.Fatalf("tests %+v %v", tests, ok)
	}
	dump, ok := st.Get("JSONDUMP")
	if !ok || dump.Status != "superseded" {
		t.Fatalf("dump %+v %v", dump, ok)
	}
	for _, id := range []string{"R1909", "R1915", "R1903", "R1922", "R1743", "R1957", "R2010", "R2013", "R2019", "STILLSTORES", "LOOPRESIDUE", "EXAMPLEDROP", "NEVERLOSEMEMO", "UNCLOSEDPAREN"} {
		recap, ok := st.Get(id)
		if !ok || recap.Status != "superseded" {
			t.Fatalf("live recap %s %+v %v", id, recap, ok)
		}
	}
	proc, ok := st.Get("PROCRECAP")
	if !ok || proc.Status != "superseded" {
		t.Fatalf("process recap %+v %v", proc, ok)
	}
	redis, ok := st.Get("REDISOK")
	if !ok || redis.Status != "active" {
		t.Fatalf("redis %+v %v", redis, ok)
	}
	same, ok := st.Get("SAMEJOB1")
	if !ok || same.Status != "active" {
		t.Fatalf("same-failure job-1 %+v %v", same, ok)
	}
	named, ok := st.Get("NAMEDKEEP")
	if !ok || named.Status != "active" {
		t.Fatalf("named-lock keep %+v %v", named, ok)
	}
	file, ok := st.Get("FILEKEEP")
	if !ok || file.Status != "active" {
		t.Fatalf("file-lock keep %+v %v", file, ok)
	}
	testGo, ok := st.Get("TESTGOKEEP")
	if !ok || testGo.Status != "active" {
		t.Fatalf("concurrent_test.go-first keep %+v %v", testGo, ok)
	}
	they, ok := st.Get("THEYREDIS")
	if !ok || they.Status != "active" {
		t.Fatalf("they-found Redis keep %+v %v", they, ok)
	}
}

func TestPruneProjectDoesNotTouchOtherProjects(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	for _, r := range []claim.Record{
		{
			ID: "ACMESLOGAN", Type: "constraint", ProjectKey: "acme/api",
			Text:      "You can switch between them and never lose memory.",
			CreatedAt: "2026-08-19T16:40:35Z", SessionID: "s-acme", Status: "active",
			Source: "import", Harness: "grok",
		},
		{
			ID: "ACMEREAL", Type: "failed", ProjectKey: "acme/api",
			Text:  "Redis token bucket failed in src/middleware/auth.ts staging.",
			Paths: []string{"src/middleware/auth.ts"}, CreatedAt: "2026-08-19T18:00:00Z",
			SessionID: "s-acme", Status: "active", Source: "import", Harness: "grok",
		},
		{
			ID: "OTHERSLOGAN", Type: "constraint", ProjectKey: "other/repo",
			Text:      "You can switch between them and never lose memory.",
			CreatedAt: "2026-08-19T16:40:35Z", SessionID: "s-other", Status: "active",
			Source: "import", Harness: "grok",
		},
	} {
		if _, err := st.WriteClaim(r); err != nil {
			t.Fatal(err)
		}
	}
	res, err := Prune(st, "acme/api")
	if err != nil {
		t.Fatal(err)
	}
	if res.SupersededNoise != 1 {
		t.Fatalf("want 1 superseded in acme/api got %+v", res)
	}
	acme, ok := st.Get("ACMESLOGAN")
	if !ok || acme.Status != "superseded" {
		t.Fatalf("acme slogan %+v %v", acme, ok)
	}
	real, ok := st.Get("ACMEREAL")
	if !ok || real.Status != "active" {
		t.Fatalf("acme redis %+v %v", real, ok)
	}
	other, ok := st.Get("OTHERSLOGAN")
	if !ok || other.Status != "active" {
		t.Fatalf("other project was pruned %+v %v", other, ok)
	}
}
