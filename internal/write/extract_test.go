package write

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"lossless/internal/claim"
)

func TestExtractClassifiesAndDrops(t *testing.T) {
	ws := t.TempDir()
	rel := "src/middleware/auth.ts"
	if err := os.MkdirAll(filepath.Join(ws, "src/middleware"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws, rel), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	msgs := []Message{
		{Skip: true, Text: "We decided to use nothing here."},
		{Role: "assistant", Text: "Redis token bucket failed in src/middleware/auth.ts staging.", Offset: 1},
		{Role: "assistant", Text: "We decided to use jose, not jsonwebtoken, for Edge.", Offset: 2},
		{Role: "user", Text: "Always never log Authorization headers in src/middleware/auth.ts.", Offset: 3},
		{Role: "assistant", Text: "Working on billing invoices export next.", Offset: 4},
		{Role: "assistant", Text: "Now implementing billing invoices export.", Offset: 5},
		{Role: "assistant", Text: "short", Offset: 6},
		{Role: "assistant", Text: "AKIAIOSFODNN7EXAMPLE failed to compile in staging.", Offset: 7},
		{Role: "assistant", Offset: 99, Text: "unrelated no classify words here at all really."},
	}
	got := Extract(msgs, ExtractOpts{ProjectKey: "acme/api", WorkspaceRoot: ws, Harness: "grok", SessionID: "s", Source: "import"})
	types := map[string]bool{}
	for _, r := range got {
		types[r.Type] = true
		if strings.Contains(r.Text, "AKIA") {
			t.Fatal("secret claim")
		}
		if strings.Contains(r.Text, "Working on billing") {
			t.Fatalf("process-state leftover extracted: %+v", r)
		}
		if r.Type == "failed" && r.PathMtime[rel] == 0 {
			t.Fatalf("expected mtime on %s: %+v", rel, r)
		}
	}
	for _, need := range []string{"failed", "decision", "constraint", "state"} {
		if !types[need] {
			t.Fatalf("missing type %s in %+v", need, got)
		}
	}
}

func TestExtractTraceCountsSkips(t *testing.T) {
	tr := &ExtractTrace{}
	got := Extract([]Message{
		{Role: "assistant", Text: "We decided to use jose, not jsonwebtoken, for Edge.", Offset: 1},
		{Role: "assistant", Text: "short", Offset: 2},
		{Role: "assistant", Text: "unrelated no classify words here at all really.", Offset: 3},
		{Role: "assistant", Text: "1. first item without a path token here", Offset: 4},
	}, ExtractOpts{ProjectKey: "acme/api", Trace: tr})
	if len(got) != 1 || tr.Kept != 1 || tr.Drafts != 1 {
		t.Fatalf("kept=%d drafts=%d recs=%+v", tr.Kept, tr.Drafts, got)
	}
	if tr.SkipCounts["untyped"] < 1 {
		t.Fatalf("skips %+v", tr.SkipCounts)
	}
	if tr.Sentences < 3 {
		t.Fatalf("sentences %d", tr.Sentences)
	}
}

func TestExtractSkipsSkillTalkAndFragments(t *testing.T) {
	got := Extract([]Message{
		{Role: "assistant", Text: "This is still not a guarantee — a model can ignore a skill — but it is no longer “one sentence on a tool next to grep.", Offset: 1},
		{Role: "assistant", Text: "swift`, …) and looked like “this failed, so that decision is dead.", Offset: 2},
		{Role: "assistant", Text: "We decided to use jose, not jsonwebtoken, for Edge.", Offset: 3},
	}, ExtractOpts{ProjectKey: "acme/api"})
	if len(got) != 1 || !strings.Contains(got[0].Text, "jose") {
		t.Fatalf("%+v", got)
	}
}

func TestExtractKeepsDurableFromEarlyInLongSession(t *testing.T) {
	var msgs []Message
	msgs = append(msgs, Message{
		Role: "assistant", Offset: 1,
		Text: "Redis token bucket failed in src/middleware/auth.ts staging.",
	})
	for i := 0; i < 50; i++ {
		msgs = append(msgs, Message{
			Role: "assistant", Offset: int64(i + 2),
			Text: "Working on src/ui/Button.tsx hover pass " + strings.Repeat("x", i%3) + " now implementing next.",
		})
	}
	got := Extract(msgs, ExtractOpts{ProjectKey: "acme/api", SessionID: "long"})
	ok := false
	for _, r := range got {
		if r.Type == "failed" && strings.Contains(r.Text, "Redis") {
			ok = true
		}
	}
	if !ok {
		t.Fatalf("early failed dropped from long session: %+v", got)
	}
}

func TestExtractFailureInPathIsNotFailed(t *testing.T) {
	got := Extract([]Message{{
		Role: "assistant", Offset: 1,
		Text: "We decided to keep src/failure/handler.ts instead of Redis.",
	}}, ExtractOpts{ProjectKey: "acme/api"})
	for _, r := range got {
		if r.Type == "failed" {
			t.Fatalf("path word failure: %+v", r)
		}
	}
	ok := false
	for _, r := range got {
		if r.Type == "decision" && strings.Contains(r.Text, "handler.ts") {
			ok = true
		}
	}
	if !ok {
		t.Fatalf("wanted decision: %+v", got)
	}
}

func TestExtractKeepsDecisionAmongManyFaileds(t *testing.T) {
	var msgs []Message
	msgs = append(msgs, Message{
		Role: "assistant", Offset: 0,
		Text: "We decided to use jose, not jsonwebtoken, for Edge in src/middleware/auth.ts.",
	})
	for i := 0; i < 20; i++ {
		msgs = append(msgs, Message{
			Role: "assistant", Offset: int64(i + 1),
			Text: fmt.Sprintf("Helper decoy %d failed in src/other/file%d.ts during compile.", i, i),
		})
	}
	got := Extract(msgs, ExtractOpts{ProjectKey: "acme/api", SessionID: "s"})
	ok := false
	nFail := 0
	for _, r := range got {
		if r.Type == "failed" {
			nFail++
		}
		if r.Type == "decision" && strings.Contains(r.Text, "jose") {
			ok = true
		}
	}
	if !ok {
		t.Fatalf("decision starved: %+v", got)
	}
	if nFail > 5 {
		t.Fatalf("extract failed flood %d: %+v", nFail, got)
	}
}

func TestExtractCapsAtTwelve(t *testing.T) {
	var msgs []Message
	for i := 0; i < 20; i++ {
		msgs = append(msgs, Message{
			Role:   "assistant",
			Text:   "This attempt failed because of unique reason number " + strings.Repeat("x", i) + " end.",
			Offset: int64(i),
		})
	}
	got := Extract(msgs, ExtractOpts{ProjectKey: "p", SessionID: "s"})
	if len(got) > 12 {
		t.Fatalf("len=%d", len(got))
	}
}

func TestExtractErrorHandlingIsNotFailed(t *testing.T) {
	got := Extract([]Message{{
		Role: "assistant", Offset: 1,
		Text: "We decided to add error handling in src/api/errors.ts.",
	}}, ExtractOpts{ProjectKey: "acme/api"})
	for _, r := range got {
		if r.Type == "failed" {
			t.Fatalf("error handling classified failed: %+v", r)
		}
	}
	ok := false
	for _, r := range got {
		if r.Type == "decision" && strings.Contains(r.Text, "error handling") {
			ok = true
		}
	}
	if !ok {
		t.Fatalf("wanted decision: %+v", got)
	}
}

func TestExtractSkipsStatusFaileds(t *testing.T) {
	got := Extract([]Message{
		{Role: "assistant", Offset: 1, Text: "That background notification is just the local Android MediaUrlsTest run finishing (exit 0) from earlier while we fixed the CI unit-test failure."},
		{Role: "assistant", Offset: 2, Text: "Checking #3081 failure and re-pushing both."},
		{Role: "assistant", Offset: 3, Text: "If anything still looks off on device after pull-to-refresh / reinstall, say which of those four failed."},
		{Role: "assistant", Offset: 4, Text: "So the Retry button is live: it re-queues failed items and they land on the server/grid."},
		{Role: "assistant", Offset: 5, Text: "Who-reacted failed in preview."},
		{Role: "assistant", Offset: 6, Text: "**Upload Complete** sheet: **7 of 10 uploaded, 3 failed**, each with **Could not load this photo**, and a Retry that does nothing."},
		{Role: "assistant", Offset: 7, Text: "Redis token bucket failed in src/middleware/auth.ts staging."},
	}, ExtractOpts{ProjectKey: "memora/memora", SessionID: "s"})
	for _, r := range got {
		if strings.Contains(r.Text, "background notification") || strings.Contains(r.Text, "Checking #") || strings.Contains(r.Text, "which of those") || strings.Contains(r.Text, "re-queues failed") {
			t.Fatalf("status failed extracted: %+v", r)
		}
	}
	var who, upload, redis bool
	for _, r := range got {
		if r.Type == "failed" && strings.Contains(r.Text, "Who-reacted") {
			who = true
		}
		if r.Type == "failed" && strings.Contains(r.Text, "Upload Complete") {
			upload = true
		}
		if r.Type == "failed" && strings.Contains(r.Text, "Redis") {
			redis = true
		}
	}
	if !who || !upload || !redis {
		t.Fatalf("real faileds missed who=%v upload=%v redis=%v %+v", who, upload, redis, got)
	}
}

func TestExtractSkipsLiveSessionNoise(t *testing.T) {
	got := Extract([]Message{
		{Role: "assistant", Offset: 1, Text: "## Investigation: why those uploads failed"},
		{Role: "user", Offset: 2, Text: "why don't you pull up the simulator on an iphone 12 mini and see what it looks like"},
		{Role: "user", Offset: 3, Text: "i'll be stepping away so don't ask any questions just do what you think is correct."},
		{Role: "user", Offset: 4, Text: "- Don't change source"},
		{Role: "user", Offset: 5, Text: "- Don't delete data"},
		{Role: "assistant", Offset: 6, Text: "I'll check what we already decided, then install a real Grok/Claude skill as part of the product."},
		{Role: "assistant", Offset: 7, Text: "Who-reacted failed in preview."},
		{Role: "assistant", Offset: 8, Text: "Android Photos-like lightbox open uses a same-window hero overlay above NavHost instead of a hard cut."},
	}, ExtractOpts{ProjectKey: "memora/memora", SessionID: "s"})
	for _, r := range got {
		if strings.HasPrefix(strings.TrimSpace(r.Text), "#") {
			t.Fatalf("heading: %+v", r)
		}
		if strings.Contains(strings.ToLower(r.Text), "why don't you") {
			t.Fatalf("agent prompt: %+v", r)
		}
		if strings.Contains(strings.ToLower(r.Text), "don't ask") || strings.Contains(r.Text, "Don't change source") {
			t.Fatalf("session op: %+v", r)
		}
		if r.Type == "decision" && strings.HasPrefix(r.Text, "I'll check") {
			t.Fatalf("planning: %+v", r)
		}
	}
	ok := false
	for _, r := range got {
		if r.Type == "decision" && strings.Contains(r.Text, "lightbox") {
			ok = true
		}
	}
	if !ok {
		t.Fatalf("real lightbox decision missed: %+v", got)
	}
}

func TestExtractSkipsTablesAndMetaFailedTalk(t *testing.T) {
	got := Extract([]Message{
		{Role: "assistant", Offset: 1, Text: "| **Claims** | `owner/repo` | A Grok `failed` on `acme/api` is what Claude’s ask is supposed to see."},
		{Role: "assistant", Offset: 2, Text: "Force in the best failed-overlap (don't repeat burned work)."},
		{Role: "assistant", Offset: 3, Text: "Raising to 8 or 10 would make recall look better without making ranking better — and this live project already fills 5 with extract noise (markdown table rows classified as `failed`)."},
		{Role: "assistant", Offset: 4, Text: "Next I'll check whether this session is actually on tape."},
		{Role: "assistant", Offset: 5, Text: "Redis token bucket failed in src/middleware/auth.ts staging."},
	}, ExtractOpts{ProjectKey: "jbgh/lossless", SessionID: "s"})
	for _, r := range got {
		if strings.Contains(r.Text, "Claims") || strings.Contains(r.Text, "failed-overlap") || strings.Contains(r.Text, "extract noise") || strings.Contains(r.Text, "Next I'll") {
			t.Fatalf("meta extracted: %+v", r)
		}
	}
	ok := false
	for _, r := range got {
		if r.Type == "failed" && strings.Contains(r.Text, "Redis") {
			ok = true
		}
	}
	if !ok {
		t.Fatalf("real failed missed: %+v", got)
	}
}

func TestExtractSkipsHeadingsAndQuotedFixtures(t *testing.T) {
	got := Extract([]Message{
		{Role: "assistant", Offset: 1, Text: "**What you do next**"},
		{Role: "assistant", Offset: 2, Text: "- Redis limiter **failed** (twice) + warning: do not repeat"},
		{Role: "assistant", Offset: 3, Text: "> Redis token bucket failed in staging."},
		{Role: "assistant", Offset: 4, Text: "A markdown heading became a state, and quoting the Redis fixture became a failed."},
		{Role: "assistant", Offset: 5, Text: "Next I will check the store after compact."},
	}, ExtractOpts{ProjectKey: "jbgh/lossless", SessionID: "s"})
	for _, r := range got {
		if strings.Contains(r.Text, "What you do next") || strings.Contains(r.Text, "Redis limiter") || strings.Contains(r.Text, "quoting the") {
			t.Fatalf("noise extracted: %+v", r)
		}
		if r.Type == "state" && strings.Contains(r.Text, "Next I will") {
			t.Fatalf("bare next became state: %+v", r)
		}
	}
}

func TestExtractHedgingIsNotConstraint(t *testing.T) {
	got := Extract([]Message{{
		Role: "user", Offset: 1,
		Text: "I don't think we should use Mongo in src/db/client.ts.",
	}}, ExtractOpts{ProjectKey: "acme/api"})
	for _, r := range got {
		if r.Type == "constraint" {
			t.Fatalf("hedge classified constraint: %+v", r)
		}
	}
}

func TestExtractSkipsToolDumps(t *testing.T) {
	got := Extract([]Message{{
		Role: "tool", Offset: 1,
		Text: "FAIL src/pkg/foo.test.ts: assertion failed at line 12\n" + strings.Repeat("stack ", 20),
	}}, ExtractOpts{ProjectKey: "acme/api"})
	if len(got) != 0 {
		t.Fatalf("tool dump became claims: %+v", got)
	}
	keep := Extract([]Message{{
		Role: "assistant", Offset: 2,
		Text: "The foo unit test failed in src/pkg/foo.test.ts.",
	}}, ExtractOpts{ProjectKey: "acme/api"})
	if len(keep) == 0 || keep[0].Type != "failed" {
		t.Fatalf("assistant failure missed: %+v", keep)
	}
}

func TestGroundedFailed(t *testing.T) {
	if GroundedFailed("Failed work first, then what already shipped.", nil) {
		t.Fatal("slogan")
	}
	if GroundedFailed("Same failure twice pauses as no-progress.", nil) {
		t.Fatal("Same is not an identifier")
	}
	if !GroundedFailed("Failed to connect after we raised the pool.", nil) {
		t.Fatal("failed to")
	}
	if !GroundedFailed("Failure during the token-bucket refill.", nil) {
		t.Fatal("failure during")
	}
	if !GroundedFailed("Tests failed to connect after we raised the pool.", nil) {
		t.Fatal("pathless Tests failed to")
	}
	if GroundedFailed("Ask warnings were treated as blocking for further product edits.", nil) {
		t.Fatal("Ask is not an identifier")
	}
	if GroundedFailed("Live prune superseded eight noise rows.", nil) {
		t.Fatal("Live is not an identifier")
	}
	if GroundedFailed("They found real control-flow holes: budget headroom too small, a failed semver check aborting the whole batch, and a “shipped N” summary when the run just ran out of slots.", nil) {
		t.Fatal("They is not an identifier")
	}
	if GroundedFailed("One remaining active failed looks recap-like.", nil) {
		t.Fatal("One is not an identifier")
	}
	if !GroundedFailed("Redis token bucket failed in staging.", nil) {
		t.Fatal("Redis")
	}
	if !GroundedFailed("the redis pool failed", []string{"src/pool.ts"}) {
		t.Fatal("path grounds")
	}
	if !GroundedFailed("Bench failed against current testdata.", nil) {
		t.Fatal("Bench is an identifier")
	}
	if !GroundedFailed("Bench failed in testdata/bench/cases/01-auth.json", nil) {
		t.Fatal("pathful Bench")
	}
	if !GroundedFailed("Bench failed", []string{"testdata/bench/cases/01-auth.json"}) {
		t.Fatal("pathful Bench paths")
	}
}

// Live inspect --project jbgh/lossless recap bodies (2026-08-19). Copied in;
// tests must not read ~/.lossless.
const (
	liveRecap1909     = "tree: productCopy slogans not bare never; space-form Same failure twice Redis still extracts; 0.1."
	liveRecap1915     = "e_test.go lock the recap row, not a pathful named-lock failed (contrast Redis token bucket)."
	liveRecap1903     = "Redis faileds, stick-with decisions, space-form “same failure twice” job-1, and pathless `Tests failed to` still store and pack."
	liveRecap1922     = "Shipping the current tree would lock fail-close skips and recap-as-failed."
	liveRecap1743     = "They found real control-flow holes: budget headroom too small, a failed semver check aborting the whole batch, and a “shipped N” summary when the run just ran out of slots."
	liveRecap1957     = "One remaining active failed looks recap-like."
	liveRecap2010     = "Live recent 8 are slice-loop / 0.1.5 decisions and version state; `recent_noise=0`; 17:43 is not a packed failed."
	liveRecap2013     = "Inspect recent on the live store still includes the recap failed “Live recent 8 are slice-loop…”, which the uncommitted 0.1.7 gates already skip (`a packed failed` / `inspect recent 8`)."
	liveRecap2019     = "Live export still has recap-faileds in the recent window, and the tests gold yesterday’s skip phrases instead of proving that window is obey-worthy."
	namedLockKeep     = "Named locks in catchup.go stay on the session JSONL."
	fileLockKeep      = "File locks are tested in concurrent_test.go."
	testGoFirstKeep   = "concurrent_test.go-first File locks failed to acquire."
	theyFoundRedis    = "They found Redis token bucket failed in src/middleware/auth.ts staging."
	theyFoundLock     = "They found the named-lock race in catchup.go."
	liveStillStores   = "A They-found Redis/path failed still stores."
	liveLoopResidue   = "Those recaps are loop residue; the product keep is: a real They-found Redis/path failed still stores."
	liveExampleDrop   = "Example drop: They found Redis token bucket failed in src/middleware/auth.ts staging."
	liveNeverLoseMemo = "Cross-harness, switch models, never lose memo"
	liveUnclosedParen = "Non-empty must not prune other projects (memora etc."
	liveRecap2208     = "Bench failed against current testdata."
	liveRecap2157a    = "Shared `SkipProse` / `ExtractNoise` now drop the five live inspect-recap shapes that were impersonating work, so a later session or another tool is less likely to be handed a fake Redis failed."
	liveRecap2157b    = "This cut makes the durable half of the goal more true: five inspect-recap shapes no longer become tape or pack, while the gold They-found Redis failed still does."
	liveRecap2146     = "go-first, 4.0, 0.1.3 remember, Authorization, I'll stick with JWT next."
	liveRecap2149     = "Secrets path unchanged; tests copy recap bodies instead of reading ~/."
	liveRecap2049     = "The gates mostly match the listed shapes, and extract does keep the gold They-found Redis failed plus real named-lock faileds."
	liveOkFalsePush   = "If it fails, ok=false and do not push."
	liveOkFalseArt    = "If there is no installable artifact, say so in detail and ok=false — a library-only repo should still have a test you just ran; do not skip."
	versionKeep       = "version.go and CHANGELOG must match."
	jwtNextKeep       = "I'll stick with JWT next."
	// Post-prune inspect recent (2026-08-19). Copied in; tests must not read ~/.lossless.
	liveRecap2237gold    = "Gold Redis/named-lock/JWT/Tests-failed-to/concurrent_test."
	liveRecap2232keep    = "Locks still keep They-found Redis/path, named-lock, JWT next, Tests failed to, concurrent_test."
	liveRecap2227colon   = "Locks still store: They-found Redis/path, named-lock, JWT next, Tests failed to connect, concurrent_test."
	liveRecap2227judge   = "Recent 8 is not all obey-worthy (`A later session still checks out recap instead of work` plus slice-loop judge residue), so 0.3 stays open."
	liveRecap2217        = "A later session still checks out recap instead of work."
	liveRecap2243        = "They-found Redis, named-lock, I'll stick with JWT next, Tests failed to, and version.go keeps are not contains-skipped."
	liveRecap2256hyphen  = "They-found Redis, named-lock, JWT next, Tests failed to, version.go, and 4.0 still keep."
	liveRecap2255colon   = "Colon-form still-store lock-list recaps skip. A concurrent_test.go-first failed is not a go-first mash."
	liveRecap2255alone   = "Colon-form still-store lock-list recaps skip."
	liveRecapBenchGround = "Pathful Bench and Failed to still ground."
)

func TestExtractSkipsLiveResidueKeepsLocks(t *testing.T) {
	residue := Extract([]Message{{
		Role: "user", Offset: 1,
		Text: "You can switch between them and never lose memory. " +
			"Intended gap: Shipped channel is still 0.1.5: leftover. " +
			"I'll run the actual git commands instead of inferring. " +
			"A first run is the right next step. " +
			"Same failure twice pauses as no-progress. " +
			"0.1.7 extract-clean tree: slogans, I'll-run, intended-gap, same-failure-twice, process-state gated. " +
			"Tests failed on the live residue as expected. " +
			"Ask warnings were treated as blocking for further product edits: do not dump an ask JSON body as assistant text. " +
			liveRecap1909 + " " + liveRecap1915 + " " + liveRecap1903 + " " + liveRecap1922 + " " +
			liveRecap1743 + " " + liveRecap1957 + " " + liveRecap2010 + " " + liveRecap2013 + " " + liveRecap2019 + " " +
			liveStillStores + " " + liveExampleDrop + " " + liveUnclosedParen + " " +
			liveRecap2146 + " " + liveRecap2256hyphen + " " +
			"ProcessState is in SkipProse as planned; Working on … next is no longer a required kept state.",
	}}, ExtractOpts{ProjectKey: "jbgh/lossless", SessionID: "s"})
	if len(residue) != 0 {
		t.Fatalf("residue extracted: %+v", residue)
	}
	for i, recap := range []string{liveRecap1909, liveRecap1915, liveRecap1903, liveRecap1922, liveRecap1743, liveRecap1957, liveRecap2010, liveRecap2013, liveRecap2019, liveStillStores, liveExampleDrop, liveUnclosedParen, liveRecap2146, liveRecap2256hyphen} {
		got := Extract([]Message{{
			Role: "assistant", Offset: int64(i + 1), Text: recap,
		}}, ExtractOpts{ProjectKey: "acme/api", SessionID: "s"})
		if len(got) != 0 {
			t.Fatalf("live recap extracted: %q -> %+v", recap, got)
		}
	}
	got := Extract([]Message{
		{Role: "user", Offset: 1, Text: "You can switch between them and never lose memory."},
		{Role: "assistant", Offset: 2, Text: "Redis token bucket failed in src/middleware/auth.ts staging."},
		{Role: "assistant", Offset: 3, Text: "We decided to use jose, not jsonwebtoken, for Edge."},
		{Role: "user", Offset: 4, Text: "Always never log Authorization headers in src/middleware/auth.ts."},
		{Role: "assistant", Offset: 5, Text: "I'll run the actual git commands instead of inferring."},
		{Role: "assistant", Offset: 6, Text: "Same failure twice pauses as no-progress."},
		{Role: "assistant", Offset: 7, Text: "Intended gap: Shipped channel is still 0.1.5: leftover."},
		{Role: "assistant", Offset: 8, Text: "0.1.7 extract-clean tree: slogans, I'll-run, intended-gap, same-failure-twice, process-state gated; Redis faileds and stick-with kept."},
		{Role: "assistant", Offset: 9, Text: "Tests failed on the live residue as expected."},
		{Role: "assistant", Offset: 10, Text: "Same failure twice: Redis token bucket still 429 in src/middleware/auth.ts."},
		{Role: "assistant", Offset: 11, Text: "The SQLITE_BUSY failure looks flaky; I'll rerun the full eval suite to confirm."},
		{Role: "assistant", Offset: 12, Text: liveRecap1903},
		{Role: "assistant", Offset: 13, Text: "ProcessState is in SkipProse as planned; Working on … next is no longer a required kept state."},
		{Role: "assistant", Offset: 14, Text: "I'll stick with the parser that still extracts JWTs from cookies in src/auth.ts."},
		{Role: "assistant", Offset: 15, Text: namedLockKeep},
		{Role: "assistant", Offset: 16, Text: fileLockKeep},
	}, ExtractOpts{ProjectKey: "acme/api", SessionID: "s"})
	var redis, job1, jose, auth, jwt bool
	for _, r := range got {
		if strings.Contains(r.Text, "never lose") || strings.Contains(r.Text, "I'll run the actual") ||
			strings.Contains(r.Text, "I'll-run") || strings.Contains(r.Text, "intended-gap") ||
			strings.Contains(r.Text, "same-failure-twice") || strings.Contains(r.Text, "live residue") ||
			strings.Contains(r.Text, "pauses as no-progress") || strings.Contains(r.Text, "Intended gap") ||
			strings.Contains(r.Text, "diff_stat") ||
			strings.Contains(r.Text, "I'll rerun") || strings.Contains(r.Text, "still store and pack") ||
			strings.Contains(r.Text, "in SkipProse") || strings.Contains(r.Text, "recap-as-failed") ||
			strings.Contains(r.Text, "lock the recap row") || strings.Contains(r.Text, "control-flow holes") ||
			strings.Contains(r.Text, "recap-like") || strings.Contains(r.Text, "recent_noise") ||
			strings.Contains(r.Text, "Inspect recent on the live store") || strings.Contains(r.Text, "recap-faileds") ||
			strings.Contains(r.Text, "still stores") ||
			strings.Contains(r.Text, "Example drop:") ||
			strings.Contains(r.Text, "memora etc.") ||
			strings.Contains(r.Text, "go-first,") {
			t.Fatalf("residue kept: %+v", r)
		}
		if r.Type == "failed" && strings.Contains(r.Text, "token bucket failed in src/middleware/auth.ts staging.") {
			redis = true
		}
		if r.Type == "failed" && strings.Contains(r.Text, "still 429") {
			job1 = true
		}
		if r.Type == "decision" && strings.Contains(r.Text, "jose") {
			jose = true
		}
		if r.Type == "constraint" && strings.Contains(r.Text, "Authorization") {
			auth = true
		}
		if r.Type == "decision" && strings.Contains(r.Text, "still extracts JWTs") {
			jwt = true
		}
	}
	if !redis || !job1 || !jose || !auth || !jwt {
		t.Fatalf("locks missed redis=%v job1=%v jose=%v auth=%v jwt=%v %+v", redis, job1, jose, auth, jwt, got)
	}
	alone := Extract([]Message{{
		Role: "assistant", Offset: 1,
		Text: "Same failure twice: Redis token bucket still 429 in src/middleware/auth.ts.",
	}}, ExtractOpts{ProjectKey: "acme/api", SessionID: "s"})
	ok := false
	for _, r := range alone {
		if r.Type == "failed" && strings.Contains(r.Text, "still 429") {
			ok = true
		}
	}
	if !ok {
		t.Fatalf("space-form job-1 Redis missed: %+v", alone)
	}
	tests := Extract([]Message{{
		Role: "assistant", Offset: 1,
		Text: "Tests failed to connect after we raised the pool.",
	}}, ExtractOpts{ProjectKey: "acme/api", SessionID: "s"})
	ok = false
	for _, r := range tests {
		if r.Type == "failed" && strings.Contains(r.Text, "Tests failed to connect") {
			ok = true
		}
	}
	if !ok {
		t.Fatalf("pathless Tests failed to missed: %+v", tests)
	}
	lockFailed := Extract([]Message{
		{Role: "assistant", Offset: 1, Text: "Named locks in catchup.go failed to persist the session JSONL."},
		{Role: "assistant", Offset: 2, Text: "File locks are tested in concurrent_test.go and the acquire failed."},
		{Role: "assistant", Offset: 3, Text: testGoFirstKeep},
	}, ExtractOpts{ProjectKey: "acme/api", SessionID: "s"})
	var named, file, testGo bool
	for _, r := range lockFailed {
		if r.Type == "failed" && strings.Contains(r.Text, "Named locks in catchup.go") {
			named = true
		}
		if r.Type == "failed" && strings.Contains(r.Text, "File locks are tested") {
			file = true
		}
		if r.Type == "failed" && strings.Contains(r.Text, "concurrent_test.go-first File locks failed") {
			testGo = true
		}
	}
	if !named || !file || !testGo {
		t.Fatalf("named-lock faileds missed named=%v file=%v testGo=%v %+v", named, file, testGo, lockFailed)
	}
	glued := Extract([]Message{
		{Role: "user", Offset: 1, Text: "Look at internal/gate/gate.go and internal/write/extract.go."},
		{Role: "assistant", Offset: 2, Text: liveRecap1743},
		{Role: "assistant", Offset: 3, Text: liveRecap1957},
		{Role: "assistant", Offset: 4, Text: liveRecap2010},
		{Role: "assistant", Offset: 5, Text: liveRecap2013},
	}, ExtractOpts{ProjectKey: "jbgh/lossless", SessionID: "s"})
	if len(glued) != 0 {
		t.Fatalf("nearby paths extracted live recap: %+v", glued)
	}
	gluedMeta := Extract([]Message{
		{Role: "user", Offset: 1, Text: "Look at src/middleware/auth.ts."},
		{Role: "assistant", Offset: 2, Text: liveStillStores},
		{Role: "assistant", Offset: 3, Text: liveExampleDrop},
		{Role: "assistant", Offset: 4, Text: liveUnclosedParen},
		{Role: "assistant", Offset: 5, Text: liveRecap2146},
		{Role: "assistant", Offset: 6, Text: liveRecap2256hyphen},
	}, ExtractOpts{ProjectKey: "acme/api", SessionID: "s"})
	if len(gluedMeta) != 0 {
		t.Fatalf("nearby auth.ts glued onto still-stores meta: %+v", gluedMeta)
	}
	theyFound := Extract([]Message{
		{Role: "user", Offset: 1, Text: "Look at src/middleware/auth.ts and catchup.go."},
		{Role: "assistant", Offset: 2, Text: theyFoundRedis},
		{Role: "assistant", Offset: 3, Text: "They found the named-lock race failed in catchup.go."},
		{Role: "assistant", Offset: 4, Text: theyFoundLock},
		{Role: "assistant", Offset: 5, Text: liveRecap1743},
		{Role: "assistant", Offset: 6, Text: liveRecap2010},
		{Role: "assistant", Offset: 7, Text: liveStillStores},
		{Role: "assistant", Offset: 8, Text: liveExampleDrop},
		{Role: "assistant", Offset: 9, Text: liveUnclosedParen},
		{Role: "assistant", Offset: 10, Text: liveRecap2146},
		{Role: "assistant", Offset: 11, Text: liveRecap2256hyphen},
		{Role: "assistant", Offset: 12, Text: jwtNextKeep},
		{Role: "user", Offset: 13, Text: versionKeep},
	}, ExtractOpts{ProjectKey: "acme/api", SessionID: "s"})
	var foundRedis, foundLock, foundJWT, foundVer bool
	for _, r := range theyFound {
		if strings.Contains(r.Text, "control-flow holes") || strings.Contains(r.Text, "recent_noise") ||
			strings.Contains(r.Text, "Inspect recent on the live store") || strings.Contains(r.Text, "still stores") ||
			strings.Contains(r.Text, "Example drop:") ||
			strings.Contains(r.Text, "memora etc.") ||
			strings.Contains(r.Text, "go-first,") {
			t.Fatalf("review recap kept next to they-found: %+v", r)
		}
		if r.Type == "failed" && strings.Contains(r.Text, "They found Redis token bucket failed") {
			foundRedis = true
		}
		if r.Type == "failed" && strings.Contains(r.Text, "named-lock race failed") {
			foundLock = true
		}
		if r.Type == "decision" && r.Text == jwtNextKeep {
			foundJWT = true
		}
		if r.Type == "constraint" && r.Text == versionKeep {
			foundVer = true
		}
	}
	if !foundRedis || !foundLock || !foundJWT || !foundVer {
		t.Fatalf("they-found keeps missed redis=%v lock=%v jwt=%v ver=%v %+v", foundRedis, foundLock, foundJWT, foundVer, theyFound)
	}
	pathfulBench := Extract([]Message{{
		Role: "assistant", Offset: 1,
		Text: "Bench failed in testdata/bench/cases/01-auth.json.",
	}}, ExtractOpts{ProjectKey: "acme/api", SessionID: "s"})
	ok = false
	for _, r := range pathfulBench {
		if r.Type == "failed" && strings.Contains(r.Text, "Bench failed in testdata") {
			ok = true
		}
	}
	if !ok {
		t.Fatalf("pathful Bench missed: %+v", pathfulBench)
	}
	sessionKeep := Extract([]Message{
		{Role: "assistant", Offset: 1, Text: "They found Redis token bucket failed in this session in src/middleware/auth.ts."},
		{Role: "assistant", Offset: 2, Text: "I'll stick with JWT next."},
		{Role: "assistant", Offset: 3, Text: "We'll use postgres next."},
		{Role: "assistant", Offset: 4, Text: "Working on billing invoices export next."},
		{Role: "assistant", Offset: 5, Text: "A They-found + Redis/path failed still stores and packs."},
	}, ExtractOpts{ProjectKey: "acme/api", SessionID: "s"})
	var inSession, stickNext, postgresNext bool
	for _, r := range sessionKeep {
		if strings.Contains(r.Text, "still stores and packs") {
			t.Fatalf("meta recap kept: %+v", r)
		}
		if r.Type == "failed" && strings.Contains(r.Text, "in this session") {
			inSession = true
		}
		if r.Type == "decision" && strings.Contains(r.Text, "I'll stick with JWT next.") {
			stickNext = true
		}
		if r.Type == "decision" && strings.Contains(r.Text, "We'll use postgres next.") {
			postgresNext = true
		}
	}
	if !inSession || !stickNext || !postgresNext {
		t.Fatalf("process-state SkipProse over-drop inSession=%v stickNext=%v postgresNext=%v %+v", inSession, stickNext, postgresNext, sessionKeep)
	}
}

func TestExtractSkipsActiveCheckoutMarkdown(t *testing.T) {
	body := "# lossless\n\nproject: acme/api\n\nRead this or call ask.\n\n## context\n\n### failed\n> Redis token bucket failed in src/middleware/auth.ts staging.\n"
	got := Extract([]Message{
		{Role: "assistant", Offset: 1, Text: body},
	}, ExtractOpts{ProjectKey: "acme/api"})
	for _, r := range got {
		if strings.Contains(r.Text, "Redis") {
			t.Fatalf("active checkout extracted: %+v", r)
		}
	}
}

func TestExtractSkipsReadmeProse(t *testing.T) {
	got := Extract([]Message{
		{Role: "assistant", Offset: 1, Text: "Compact thinning is failed approaches become a clause; a library choice becomes picked something."},
		{Role: "assistant", Offset: 2, Text: "Over a long project that happens again and again, so failed approaches and shipped decisions disappear."},
		{Role: "assistant", Offset: 3, Text: "Failed work first, then what already shipped."},
		{Role: "assistant", Offset: 4, Text: "Redis token bucket failed in src/middleware/auth.ts staging."},
	}, ExtractOpts{ProjectKey: "acme/api"})
	for _, r := range got {
		if strings.Contains(r.Text, "Compact thinning") || strings.Contains(r.Text, "long project") || strings.Contains(r.Text, "Failed work first") {
			t.Fatalf("readme prose extracted: %+v", r)
		}
	}
	ok := false
	for _, r := range got {
		if r.Type == "failed" && strings.Contains(r.Text, "Redis") {
			ok = true
		}
	}
	if !ok {
		t.Fatalf("real failed missed: %+v", got)
	}
}

func TestExtractSkipsAdviceFailureTalk(t *testing.T) {
	got := Extract([]Message{
		{Role: "assistant", Offset: 1, Text: "** Each of those five is a stand-in for a real failure."},
		{Role: "assistant", Offset: 2, Text: "Fix the failure at the layer that caused it."},
		{Role: "assistant", Offset: 3, Text: "Redis connection failure in src/middleware/auth.ts."},
	}, ExtractOpts{ProjectKey: "acme/api"})
	for _, r := range got {
		if strings.Contains(r.Text, "stand-in") || strings.Contains(r.Text, "layer that") {
			t.Fatalf("advice failed extracted: %+v", r)
		}
	}
	ok := false
	for _, r := range got {
		if r.Type == "failed" && strings.Contains(r.Text, "Redis") {
			ok = true
		}
	}
	if !ok {
		t.Fatalf("real connection failure missed: %+v", got)
	}
}

func TestExtractCodingSessionPhrases(t *testing.T) {
	got := Extract([]Message{
		{Role: "assistant", Offset: 1, Text: "Use jose, not jsonwebtoken, for Edge in src/middleware/auth.ts."},
		{Role: "assistant", Offset: 2, Text: "Prefer postgres over mysql in src/db/client.ts."},
		{Role: "assistant", Offset: 3, Text: "Stick with the in-process limiter in src/middleware/auth.ts."},
		{Role: "assistant", Offset: 4, Text: "The auth unit tests don't pass in src/middleware/auth.test.ts."},
		{Role: "assistant", Offset: 5, Text: "Use the next not because we ran out of time."},
		{Role: "user", Offset: 6, Text: "Should we use jose, not jsonwebtoken?"},
	}, ExtractOpts{ProjectKey: "acme/api"})
	var jose, pg, stick, tests bool
	for _, r := range got {
		if r.Type == "decision" && strings.Contains(r.Text, "jose") {
			jose = true
		}
		if r.Type == "decision" && strings.Contains(r.Text, "postgres") {
			pg = true
		}
		if r.Type == "decision" && strings.Contains(strings.ToLower(r.Text), "stick") {
			stick = true
		}
		if r.Type == "failed" && strings.Contains(r.Text, "don't pass") {
			tests = true
		}
		if strings.Contains(r.Text, "next not") {
			t.Fatalf("use-the-next extracted: %+v", r)
		}
		if strings.HasPrefix(r.Text, "Should we") {
			t.Fatalf("question extracted: %+v", r)
		}
	}
	if !jose || !pg || !stick || !tests {
		t.Fatalf("coding phrases missed jose=%v pg=%v stick=%v tests=%v %+v", jose, pg, stick, tests, got)
	}
}

func TestExtractQuestionIsNotDecisionOrConstraint(t *testing.T) {
	got := Extract([]Message{{
		Role: "user", Offset: 1,
		Text: "Should we use postgres in src/db/client.ts?",
	}}, ExtractOpts{ProjectKey: "acme/api"})
	for _, r := range got {
		if r.Type == "decision" || r.Type == "constraint" {
			t.Fatalf("question classified %s: %+v", r.Type, r)
		}
	}
}

func TestExtractDoesNotMarkHypotheticalAsFailed(t *testing.T) {
	got := Extract([]Message{{
		Role:   "assistant",
		Text:   "I was going to try jsonwebtoken unless we already rejected that.",
		Offset: 1,
	}}, ExtractOpts{ProjectKey: "acme/api"})
	for _, r := range got {
		if r.Type == "failed" {
			t.Fatalf("hypothetical classified failed: %+v", r)
		}
	}
	real := Extract([]Message{{
		Role:   "assistant",
		Text:   "We rejected Redis for rate limiting in src/middleware/auth.ts.",
		Offset: 2,
	}}, ExtractOpts{ProjectKey: "acme/api"})
	ok := false
	for _, r := range real {
		if r.Type == "failed" && strings.Contains(r.Text, "Redis") {
			ok = true
		}
	}
	if !ok {
		t.Fatalf("real rejection missed: %+v", real)
	}
}

func TestExtractDedupPrefersFailed(t *testing.T) {
	// same normalized text, different classify — failed should win
	text := "We decided this failed to compile for Edge runtime."
	got := Extract([]Message{
		{Role: "assistant", Text: text, Offset: 1},
	}, ExtractOpts{ProjectKey: "p"})
	if len(got) != 1 || got[0].Type != "failed" {
		t.Fatalf("%+v", got)
	}
}

func TestClassifyAndHelpers(t *testing.T) {
	if classify("it didn't work today at all", Message{}) != "failed" {
		t.Fatal("failed")
	}
	if classify("ok", Message{Error: true}) != "failed" {
		t.Fatal("error flag")
	}
	if classify("We decided to revert the Redis limiter and keep jose.", Message{}) != "decision" {
		t.Fatal("decided-to-revert")
	}
	if classify("That's an exception to the rule we always use jose.", Message{}) != "" {
		t.Fatal("exception-to")
	}
	if classify("We decided to use jose, not jsonwebtoken, for Edge.", Message{Error: true}) != "decision" {
		t.Fatal("error flag must not stomp decision")
	}
	if classify("we will use jose instead of x", Message{}) != "decision" {
		t.Fatal("decision")
	}
	if classify("Always use jose for this", Message{Role: "user"}) != "constraint" {
		t.Fatal("constraint")
	}
	if classify("Always use jose for this", Message{Role: "assistant"}) != "" {
		t.Fatal("constraint is user-only")
	}
	if classify("Now implementing the limiter next", Message{}) != "state" {
		t.Fatal("state")
	}
	if classify("hello there friend", Message{}) != "" {
		t.Fatal("none")
	}

	if nearby(Message{Offset: 99}, []Message{{Offset: 1}}) != nil {
		t.Fatal("nearby miss")
	}
	paths := nearby(Message{Role: "assistant", Offset: 2}, []Message{
		{Role: "user", Offset: 1, Text: "see src/a.ts please"},
		{Role: "assistant", Offset: 2, Text: "Redis token bucket failed in staging."},
	})
	if len(paths) == 0 {
		t.Fatal("nearby hit")
	}

	long := make([]Message, 3)
	for i := range long {
		long[i] = Message{Text: strings.Repeat("z", 100)}
	}
	if n := tail(long, 40, 50); len(n) < 1 {
		t.Fatal("tail")
	}

	if len(uniq([]string{"", "a", "a", "b"})) != 2 {
		t.Fatal("uniq")
	}
	recs := []claim.Record{{Type: "state"}, {Type: "failed"}, {Type: "decision"}}
	sortByPri(recs)
	if recs[0].Type != "failed" || recs[1].Type != "decision" {
		t.Fatalf("%+v", recs)
	}

	r := makeRec("decision", "Use jose, not jsonwebtoken, for Edge.", []string{
		"a.ts", "b.ts", "c.ts", "d.ts", "e.ts", "f.ts", "g.ts", "h.ts", "i.ts",
	}, Message{Offset: 0, Text: "Use jose, not jsonwebtoken, for Edge."}, ExtractOpts{})
	if len(r.Paths) != 8 {
		t.Fatal(len(r.Paths))
	}
}

func TestExtractManyPathsAndDropSensitive(t *testing.T) {
	var paths []string
	for i := 0; i < 10; i++ {
		paths = append(paths, "src/pkg/file"+string(rune('a'+i))+".ts")
	}
	text := "We decided to keep the limiter. See " + strings.Join(paths, " ") + "."
	got := Extract([]Message{{Role: "assistant", Text: text, Offset: 1}}, ExtractOpts{ProjectKey: "p"})
	if len(got) == 0 {
		t.Fatal("expected decision")
	}
	drop := Extract([]Message{{
		Role: "user", Text: "Always put SECRET in .env for local dev now.", Offset: 1,
	}}, ExtractOpts{ProjectKey: "p"})
	for _, r := range drop {
		if strings.Contains(r.Text, "SECRET") && len(r.Paths) > 0 {
			t.Fatalf("should drop: %+v", r)
		}
	}
}

func TestSplitSentences(t *testing.T) {
	got := splitSentences("One. Two!\nThree?")
	if len(got) < 3 {
		t.Fatal(got)
	}
	if len(splitSentences("no terminator")) != 1 {
		t.Fatal(splitSentences("no terminator"))
	}
	list := splitSentences("1. Redis limiter **failed** (twice) + warning: do not repeat")
	if len(list) != 1 || !strings.HasPrefix(list[0], "1. ") {
		t.Fatalf("numbered list split: %#v", list)
	}
	still := splitSentences("Stopped at 12. Then we kept going on src/a.ts.")
	if len(still) < 2 {
		t.Fatalf("mid-sentence number should still split: %#v", still)
	}
}

func TestSplitDoesNotBreakOnFileExtension(t *testing.T) {
	text := "The limiter stays in-process in src/middleware/auth.ts instead of Redis."
	got := splitSentences(text)
	if len(got) != 1 {
		t.Fatalf("split on .ts: %#v", got)
	}
	hyphen := splitSentences(testGoFirstKeep)
	if len(hyphen) != 1 {
		t.Fatalf("split on .go-first: %#v", hyphen)
	}
	recs := Extract([]Message{{Role: "assistant", Text: text, Offset: 1}}, ExtractOpts{ProjectKey: "acme/api"})
	for _, r := range recs {
		if strings.HasPrefix(strings.TrimSpace(r.Text), "ts ") {
			t.Fatalf("chopped claim: %q", r.Text)
		}
	}
	joined := ""
	for _, r := range recs {
		joined += r.Text
	}
	if !strings.Contains(joined, "instead of Redis") || !strings.Contains(joined, "auth.ts") {
		t.Fatalf("lost sentence: %+v", recs)
	}
}
