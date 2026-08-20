package retrieve

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"lossless/internal/claim"
)

func TestNormalizeAndHelpers(t *testing.T) {
	q, err := normalize(Request{
		Project:     "Acme/API",
		Question:    `why not "jsonwebtoken"?`,
		Goal:        "pick a jwt library",
		Paths:       []string{"src/middleware/auth.ts"},
		LimitTokens: 0,
	})
	if err != nil || q.ProjectKey != "acme/api" || q.Cold || q.LimitTokens != DefaultLimit {
		t.Fatalf("%+v %v", q, err)
	}
	if len(q.PathKeys) < 2 || len(q.Symbols) == 0 || len(q.LookupTokens) == 0 {
		t.Fatalf("paths/symbols/lookup: %+v", q)
	}
	if _, err := normalize(Request{}); err == nil {
		t.Fatal("bad request")
	}
	dir := t.TempDir()
	cmd := exec.Command("git", "init")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatal(string(out))
	}
	cmd = exec.Command("git", "remote", "add", "origin", "https://github.com/Acme/API.git")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatal(string(out))
	}
	q, err = normalize(Request{WorkspaceRoot: dir})
	if err != nil || q.ProjectKey != "acme/api" || !q.Cold {
		t.Fatalf("%+v %v", q, err)
	}

	if identLower("ab") || identLower("1abc") || identLower("ab-c") {
		t.Fatal("identLower false")
	}
	if !identLower("jose") || !identLower("_id") {
		t.Fatal("identLower true")
	}
	if ftsMatch([]string{`"`, "jose", "  "}) != `"jose"` {
		t.Fatal(ftsMatch([]string{`"`, "jose", "  "}))
	}
	if jaccard(nil, []string{"a"}) != 0 || jaccard([]string{""}, []string{""}) != 0 {
		t.Fatal("jaccard empty")
	}
	if jaccard([]string{"a", "b"}, []string{"b", "c"}) != 1.0/3.0 {
		t.Fatal(jaccard([]string{"a", "b"}, []string{"b", "c"}))
	}
	if estimateTokens("") != 1 || estimateTokens("abcd") != 1 || estimateTokens("abcde") != 2 {
		t.Fatal("tokens")
	}
	if uniq([]string{"", "a", "a", "b"})[0] != "a" {
		t.Fatal(uniq([]string{"", "a", "a", "b"}))
	}
	q, err = normalize(Request{Project: "acme/api", Question: "JWT library choice"})
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range q.Symbols {
		if s == "library" || s == "choice" {
			t.Fatalf("english leftover leaked into symbols: %v", q.Symbols)
		}
	}
	foundJWT := false
	for _, s := range q.Symbols {
		if s == "jwt" || s == "jsonwebtoken" {
			foundJWT = true
		}
	}
	if !foundJWT {
		t.Fatalf("jwt should remain a symbol: %v", q.Symbols)
	}
	_ = os.Remove(filepath.Join(dir, "nope"))
}

func TestMustJSONAndToHit(t *testing.T) {
	if mustJSON(make(chan int)) != "" {
		t.Fatal("chan should fail marshal")
	}
	h := toHit(scored{rec: claim.Record{ID: "I", Type: "decision", Text: "t"}}, false)
	if h.Paths == nil {
		t.Fatal("nil paths")
	}
}

func TestExtractNoiseAndJobOverlap(t *testing.T) {
	if !extractNoise(claim.Record{Text: "| **Claims** | owner/repo | A Grok failed |"}) {
		t.Fatal("table")
	}
	if !extractNoise(claim.Record{Text: "Force in the best failed-overlap now."}) {
		t.Fatal("failed-overlap")
	}
	if !extractNoise(claim.Record{Type: "state", Text: "Next I'll check the tape after compact."}) {
		t.Fatal("next I'll")
	}
	if extractNoise(claim.Record{Type: "failed", Text: "Redis token bucket failed in staging.", Paths: []string{"src/middleware/auth.ts"}}) {
		t.Fatal("real failed")
	}
	if !extractNoise(claim.Record{Type: "failed", Text: "Failed work first, then what already shipped."}) {
		t.Fatal("readme failed-first ungrounded")
	}
	if extractNoise(claim.Record{Type: "failed", Text: "Failed to connect after we raised the pool."}) {
		t.Fatal("failed to connect")
	}
	if extractNoise(claim.Record{Type: "failed", Text: "Redis token bucket failed in staging."}) {
		t.Fatal("Redis still grounds a pathless failed")
	}
	if extractNoise(claim.Record{Type: "failed", Text: "The pack filter was too blunt: pathless two-hop still needs the Redis failed."}) {
		t.Fatal("two-hop redis failed")
	}
	if !extractNoise(claim.Record{Type: "failed", Text: "The live ask just returned five `failed`s, and four look like extract noise."}) {
		t.Fatal("backticked type mention")
	}
	if !extractNoise(claim.Record{Type: "failed", Text: "## Investigation: why those uploads failed"}) {
		t.Fatal("heading")
	}
	if !extractNoise(claim.Record{Type: "constraint", Text: "the scrubbing video doesn't seem to work why don't you verify on the emulator"}) {
		t.Fatal("why don't you")
	}
	if !extractNoise(claim.Record{Type: "constraint", Text: "- Don't change source"}) {
		t.Fatal("short bullet")
	}
	if !extractNoise(claim.Record{Type: "decision", Text: "I'll check what we already decided, then install a skill."}) {
		t.Fatal("planning")
	}
	if extractNoise(claim.Record{Type: "decision", Text: "I'll cold-start at game size × 2 instead of resizing mid-session."}) {
		t.Fatal("real I'll decision")
	}
	if !extractNoise(claim.Record{Type: "failed", Text: "That background notification is just the local Android MediaUrlsTest run finishing (exit 0) while we fixed the CI unit-test failure."}) {
		t.Fatal("ci status")
	}
	if !extractNoise(claim.Record{Type: "failed", Text: "Compact thinning is failed approaches become a clause."}) {
		t.Fatal("readme prose")
	}
	if !extractNoise(claim.Record{Type: "failed", Text: "Checking #3081 failure and re-pushing both.", Paths: []string{"git.memora.pics/memora/memora.git"}}) {
		t.Fatal("checking #")
	}
	if !extractNoise(claim.Record{Type: "state", Text: "So the next test that matters is not another fixture."}) {
		t.Fatal("process state")
	}
	if !extractNoise(claim.Record{Type: "state", Text: "This is still not a guarantee — a model can ignore a skill — but it is no longer “one sentence on a tool next to grep.", Paths: []string{".claude/skills/lossless/SKILL.md"}}) {
		t.Fatal("skill-state")
	}
	if extractNoise(claim.Record{Type: "failed", Text: "**Upload Complete** sheet: 7 of 10 uploaded, 3 failed.", Paths: []string{}}) {
		t.Fatal("real upload failed")
	}
	for _, rec := range []claim.Record{
		{Type: "constraint", Text: "You can switch between them and never lose memory."},
		{Type: "constraint", Text: "you can switch between them and never lose your memory."},
		{Type: "constraint", Text: "Intended gap: Shipped channel is still 0.1.5: OpenCode plugin-miss never reach the tape."},
		{Type: "decision", Text: "Git objects are binary — I’ll run the actual git commands instead of inferring from object IDs."},
		{Type: "state", Text: "A first run from the **lossless checkout**, with the example args, is the right next step."},
		{Type: "failed", Text: "Same failure twice pauses as no-progress."},
		{Type: "failed", Text: "The SQLITE_BUSY failure looks flaky; I'll rerun the full eval suite to confirm."},
		{Type: "failed", Text: "0.1.7 extract-clean tree: slogans, I'll-run, intended-gap, same-failure-twice, process-state gated; Redis faileds and stick-with kept."},
		{Type: "failed", Text: "Tests failed on the live residue as expected."},
		{Type: "failed", Text: "Ask warnings were treated as blocking for further product edits: do not dump an ask JSON body as assistant text; keep-tests (mobile SDK, failed worker) stay."},
		{Type: "failed", Text: "Live prune superseded 8 noise rows (slogans, I'll-run, intended-gap, right-next-step, same-failure-twice)."},
		{Type: "failed", Text: "Slogans, I'll-run, intended-gap, right-next-step, same-failure-twice, and the test-pass recap are gone."},
		{Type: "failed", Text: `{"diff_stat":"13 files changed, 322 insertions(+), 6 deletions(-)","ok":true,"summary":"Gate SkipProse now treats hyphenated I'll-run."}`},
		{Type: "state", Text: "ProcessState is in SkipProse as planned; Working on … next is no longer a required kept state."},
		{Type: "failed", Text: "tree: productCopy slogans not bare never; space-form Same failure twice Redis still extracts; 0.1."},
		{Type: "failed", Text: "e_test.go lock the recap row, not a pathful named-lock failed (contrast Redis token bucket)."},
		{Type: "failed", Text: "Redis faileds, stick-with decisions, space-form “same failure twice” job-1, and pathless `Tests failed to` still store and pack."},
		{Type: "failed", Text: "Shipping the current tree would lock fail-close skips and recap-as-failed."},
		{Type: "failed", Text: "They found real control-flow holes: budget headroom too small, a failed semver check aborting the whole batch, and a “shipped N” summary when the run just ran out of slots."},
		{Type: "failed", Text: "One remaining active failed looks recap-like."},
		{Type: "failed", Text: "Live recent 8 are slice-loop / 0.1.5 decisions and version state; `recent_noise=0`; 17:43 is not a packed failed."},
		{Type: "failed", Text: "Inspect recent on the live store still includes the recap failed “Live recent 8 are slice-loop…”, which the uncommitted 0.1.7 gates already skip (`a packed failed` / `inspect recent 8`)."},
		{Type: "decision", Text: "Live export still has recap-faileds in the recent window, and the tests gold yesterday’s skip phrases instead of proving that window is obey-worthy."},
		{Type: "failed", Text: "A They-found Redis/path failed still stores."},
		{Type: "failed", Text: "Those recaps are loop residue; the product keep is: a real They-found Redis/path failed still stores."},
		{Type: "failed", Text: "Example drop: They found Redis token bucket failed in src/middleware/auth.ts staging.", Paths: []string{"src/middleware/auth.ts"}},
		{Type: "constraint", Text: "Non-empty must not prune other projects (memora etc."},
		{Type: "decision", Text: "go-first, 4.0, 0.1.3 remember, Authorization, I'll stick with JWT next."},
		{Type: "failed", Text: "They-found Redis, named-lock, JWT next, Tests failed to, version.go, and 4.0 still keep."},
	} {
		if !extractNoise(rec) {
			t.Fatalf("residue not noise: %+v", rec)
		}
	}
	if extractNoise(claim.Record{Type: "failed", Text: "Redis token bucket failed in staging.", Paths: []string{"src/middleware/auth.ts"}}) {
		t.Fatal("Redis failed lock")
	}
	if extractNoise(claim.Record{Type: "decision", Text: "I'll stick with postgres instead of mysql."}) {
		t.Fatal("I'll stick with lock")
	}
	if extractNoise(claim.Record{Type: "failed", Text: "0.1.3 / 0.3 extract-clean: gate failed-work-first. Real pathful faileds stay."}) {
		t.Fatal("0.1.3 remember lock")
	}
	if extractNoise(claim.Record{Type: "failed", Text: "Same failure twice: Redis token bucket still 429 in staging.", Paths: []string{"src/middleware/auth.ts"}}) {
		t.Fatal("same-failure job-1 lock")
	}
	if extractNoise(claim.Record{Type: "failed", Text: "Tests failed to connect after we raised the pool."}) {
		t.Fatal("pathless Tests failed to lock")
	}
	if extractNoise(claim.Record{Type: "decision", Text: "I'll stick with the parser that still extracts JWTs from cookies in src/auth.ts."}) {
		t.Fatal("JWT still extracts lock")
	}
	if extractNoise(claim.Record{Type: "failed", Text: "Named locks in catchup.go stay on the session JSONL."}) {
		t.Fatal("named-lock keep")
	}
	if extractNoise(claim.Record{Type: "failed", Text: "File locks are tested in concurrent_test.go."}) {
		t.Fatal("file-lock keep")
	}
	if extractNoise(claim.Record{Type: "failed", Text: "concurrent_test.go-first File locks failed to acquire."}) {
		t.Fatal("concurrent_test.go-first keep")
	}
	if extractNoise(claim.Record{Type: "failed", Text: "Named locks still keep the session JSONL in catchup.go.", Paths: []string{"catchup.go"}}) {
		t.Fatal("still keep + object keep")
	}
	if extractNoise(claim.Record{Type: "failed", Text: "WFailedOverlap stays 4.0."}) {
		t.Fatal("standing 4.0 keep")
	}
	if extractNoise(claim.Record{Type: "failed", Text: "They found Redis token bucket failed in src/middleware/auth.ts staging.", Paths: []string{"src/middleware/auth.ts"}}) {
		t.Fatal("they-found Redis keep")
	}
	if extractNoise(claim.Record{Type: "failed", Text: "They found the named-lock race in catchup.go.", Paths: []string{"catchup.go"}}) {
		t.Fatal("they-found named-lock keep")
	}
	if extractNoise(claim.Record{Type: "failed", Text: "They found Redis token bucket failed in this session in src/middleware/auth.ts.", Paths: []string{"src/middleware/auth.ts"}}) {
		t.Fatal("they-found Redis in this session keep")
	}
	if extractNoise(claim.Record{Type: "decision", Text: "I'll stick with JWT next."}) {
		t.Fatal("stick-with next keep")
	}
	if extractNoise(claim.Record{Type: "constraint", Text: "version.go and CHANGELOG must match."}) {
		t.Fatal("version.go CHANGELOG keep")
	}
	if extractNoise(claim.Record{Type: "failed", Text: "Bench failed in testdata/bench/cases/01-auth.json", Paths: []string{"testdata/bench/cases/01-auth.json"}}) {
		t.Fatal("pathful Bench keep")
	}
	if extractNoise(claim.Record{Type: "failed", Text: "Bench failed against current testdata."}) {
		t.Fatal("pathless Bench keep")
	}
	if extractNoise(claim.Record{Type: "failed", Text: "Login returned ok=false in src/auth.ts after we rotated tokens.", Paths: []string{"src/auth.ts"}}) {
		t.Fatal("ok=false health failed keep")
	}
	if extractNoise(claim.Record{Type: "failed", Text: "This cut makes the Redis limiter fail in src/middleware/auth.ts.", Paths: []string{"src/middleware/auth.ts"}}) {
		t.Fatal("this cut makes Redis keep")
	}
	if extractNoise(claim.Record{Type: "constraint", Text: "Never lose memoization of JWT claims in src/auth.ts.", Paths: []string{"src/auth.ts"}}) {
		t.Fatal("never lose memoization keep")
	}
	if extractNoise(claim.Record{Type: "failed", Text: "We still store: the session JSONL on disk in catchup.go.", Paths: []string{"catchup.go"}}) {
		t.Fatal("colon still store keep")
	}
	if extractNoise(claim.Record{Type: "decision", Text: "We'll use postgres next."}) {
		t.Fatal("we'll use postgres next keep")
	}
	if extractNoise(claim.Record{Type: "failed", Text: "Named locks in catchup.go failed to persist the session JSONL.", Paths: []string{"catchup.go"}}) {
		t.Fatal("named-lock failed keep")
	}
	if !extractNoise(claim.Record{Type: "failed", Text: "A They-found + Redis/path failed still stores and packs."}) {
		t.Fatal("still stores and packs meta")
	}
	if !extractNoise(claim.Record{Type: "failed", Text: "A They-found Redis/path failed still stores."}) {
		t.Fatal("still stores. without and pack")
	}
	if !extractNoise(claim.Record{Type: "failed", Text: "Example drop: They found Redis token bucket failed in src/middleware/auth.ts staging.", Paths: []string{"src/middleware/auth.ts"}}) {
		t.Fatal("example drop pathful redis")
	}
	if !extractNoise(claim.Record{Type: "state", Text: "Working on billing invoices export next."}) {
		t.Fatal("process-state leftover as state")
	}
	qtoks := []string{"retrieve", "extract", "failed", "noise", "pack"}
	if contentOverlap(qtoks, jobOverlapText("Raising to 8 would make recall look better — extract noise classified as `failed`.")) >= OverlapStrongMin {
		t.Fatal("meta should not be strong job overlap")
	}
	if contentOverlap([]string{"redis", "limiter", "auth"}, jobOverlapText("Redis token bucket failed in src/middleware/auth.ts staging.")) < 1 {
		t.Fatal("real failed should still overlap redis")
	}
}
