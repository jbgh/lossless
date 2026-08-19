package gate

import "testing"

func TestFoldCurlyApos(t *testing.T) {
	if Fold("Don’t wait") != "don't wait" {
		t.Fatalf("%q", Fold("Don’t wait"))
	}
}

func TestPlanningDoesNotHitColdStart(t *testing.T) {
	if Planning("I'll cold-start at game size x 2 instead of resizing mid-session.") {
		t.Fatal("cold-start")
	}
	if !Planning("I'll start with brainstorming instead of slides.") {
		t.Fatal("i'll start")
	}
	if !Planning("I'll go with postgres instead of mysql.") {
		t.Fatal("go with")
	}
}

func TestSessionOpFoldsDont(t *testing.T) {
	if !SessionOp("Don’t wait for me, just keep going.") {
		t.Fatal("curly don't wait")
	}
	if !SessionOp("Never mind the lint.") {
		t.Fatal("never mind")
	}
}

func TestYAMLAndList(t *testing.T) {
	if !YAMLClaimChrome(`text = "Redis token bucket failed"`) {
		t.Fatal("toml text")
	}
	if !YAMLClaimChrome("text=Redis token bucket failed") {
		t.Fatal("ini text")
	}
	rest, ok := ListMarker("1. Redis limiter **failed** (twice)")
	if !ok || rest == "" {
		t.Fatal("numbered")
	}
	if !ListChrome("1. Redis limiter **failed** (twice)", false) {
		t.Fatal("bold numbered")
	}
}

func TestLivePackLeftovers(t *testing.T) {
	if !MetaFailedTalk(`- Off-topic but real memora "Sendable tests failed" — ranking/topic, next only if you want that slice`) {
		t.Fatal("off-topic bullet")
	}
	if !ConstraintFragment("so the prompt you store must stand on its own.") {
		t.Fatal("so-fragment")
	}
	if !ProcessState("Writing the design spec and the deck script next.") {
		t.Fatal("writing next")
	}
}

func TestSkipProse(t *testing.T) {
	if !SkipProse("I'll check what we already decided.") {
		t.Fatal("planning")
	}
	if SkipProse("We decided to use jose, not jsonwebtoken, for Edge.") {
		t.Fatal("real decision")
	}
	if SkipProse("Skip-list phrases live in internal/gate (Fold curly apostrophes).") {
		t.Fatal("curly apostrophe in a real decision")
	}
	if !SkillTalk("This is still not a guarantee — a model can ignore a skill — but it is no longer “one sentence on a tool next to grep.") {
		t.Fatal("skill talk")
	}
	if !Truncated("swift`, …) and looked like “this failed, so that decision is dead.") {
		t.Fatal("mid-sentence fragment")
	}
	if !Truncated("` and trailing-slash paths; YAML `text: Redis failed…` |") {
		t.Fatal("pipe fragment")
	}
	if !ListChrome("- iOS hero thumb cache probes a fixed pixel-bucket list; if that misses, the first frame can be empty instead of the last good decode.", true) {
		t.Fatal("long pathless bullet")
	}
	if SkipProse("Always never log Authorization headers in src/middleware/auth.ts.") {
		t.Fatal("user constraint")
	}
	if !ProductCopy("Compact thinning is failed approaches become a clause.") {
		t.Fatal("readme failed")
	}
	if !SkipProse("Over a long project that happens again and again, so failed approaches disappear.") {
		t.Fatal("readme long-project")
	}
	if SkipProse("Redis token bucket failed in src/middleware/auth.ts staging.") {
		t.Fatal("real failed")
	}
	if !SkipProse("** Each of those five is a stand-in for a real failure.") {
		t.Fatal("unclosed bold + stand-in")
	}
	if !FailedAsObject("Fix the failure at the layer that caused it.") {
		t.Fatal("fix the failure")
	}
	if SkipProse("Redis connection failure in src/middleware/auth.ts.") {
		t.Fatal("real connection failure")
	}
	if !SkipProse("I'll ask lossless what's already decided, then look through the repo.") {
		t.Fatal("i'll ask planning")
	}
	if !SkipProse("I’ll ask lossless what's already decided.") {
		t.Fatal("curly i'll ask")
	}
	if SkipProse("Use the ask tool before implementing in src/app.ts.") {
		t.Fatal("real ask mention")
	}
	if SkipProse("I'll stick with postgres instead of mysql.") {
		t.Fatal("i'll stick with is a decision")
	}
	if !SkipProse("Failed work first, then what already shipped.") {
		t.Fatal("readme failed-first")
	}
	if !SkipProse("The next product is: the model sees it before it retries the failed work.") {
		t.Fatal("roadmap next-product")
	}
	if !SkipProse("Remaining recent residue (“The next product is…”, “Failed work first…”) is extract residue.") {
		t.Fatal("residue meta")
	}
	if !SkipProse(`“I’ll stick with postgres” still extracts.`) {
		t.Fatal("gate-test echo")
	}
	if !SkipProse("Harness holes beyond OpenCode watcher / Codex desktop.") {
		t.Fatal("review prompt")
	}
	if !SkipProse("First I'll load the diff, gate copy, and eval sentences.") {
		t.Fatal("i'll load planning")
	}
	if SkipProse("Redis token bucket failed in src/middleware/auth.ts staging after the limiter shipped.") {
		t.Fatal("real failed after shipped")
	}
	if SkipProse("We decided the next product is the mobile SDK, not the CLI, in packages/sdk/index.ts.") {
		t.Fatal("real next-product decision")
	}
	if SkipProse("The dispatcher retries the failed worker in src/jobs/worker.ts after Redis failed.") {
		t.Fatal("failed worker")
	}
	if SkipProse("We decided to keep what already shipped in src/middleware/auth.ts.") {
		t.Fatal("keep what already shipped")
	}
	if SkipProse("I'll stick with the parser that still extracts JWTs from cookies in src/auth.ts.") {
		t.Fatal("still extracts jwt")
	}
	if SkipProse("0.1.3 / 0.3 extract-clean: gate failed-work-first. Real pathful faileds stay.") {
		t.Fatal("0.1.3 remember")
	}
}

// Live inspect --project jbgh/lossless recap bodies (2026-08-19). Copied in;
// tests must not read ~/.lossless.
const (
	liveRecap1909   = "tree: productCopy slogans not bare never; space-form Same failure twice Redis still extracts; 0.1."
	liveRecap1915   = "e_test.go lock the recap row, not a pathful named-lock failed (contrast Redis token bucket)."
	liveRecap1903   = "Redis faileds, stick-with decisions, space-form “same failure twice” job-1, and pathless `Tests failed to` still store and pack."
	liveRecap1922   = "Shipping the current tree would lock fail-close skips and recap-as-failed."
	liveRecap1931   = "Ship only when inspect recent 8 are things a future session should obey and 19:09 is not a packed failed."
	liveRecap1743   = "They found real control-flow holes: budget headroom too small, a failed semver check aborting the whole batch, and a “shipped N” summary when the run just ran out of slots."
	liveRecap1957   = "One remaining active failed looks recap-like."
	liveRecap2010   = "Live recent 8 are slice-loop / 0.1.5 decisions and version state; `recent_noise=0`; 17:43 is not a packed failed."
	liveRecap2013   = "Inspect recent on the live store still includes the recap failed “Live recent 8 are slice-loop…”, which the uncommitted 0.1.7 gates already skip (`a packed failed` / `inspect recent 8`)."
	liveRecap2019   = "Live export still has recap-faileds in the recent window, and the tests gold yesterday’s skip phrases instead of proving that window is obey-worthy."
	namedLockKeep   = "Named locks in catchup.go stay on the session JSONL."
	fileLockKeep    = "File locks are tested in concurrent_test.go."
	testGoFirstKeep = "concurrent_test.go File locks failed to acquire."
	weightKeep      = "WFailedOverlap stays 4.0."
	theyFoundRedis  = "They found Redis token bucket failed in src/middleware/auth.ts staging."
	theyFoundLock   = "They found the named-lock race in catchup.go."
)

func TestTruncatedLiveRecaps(t *testing.T) {
	if !Truncated(liveRecap1909) {
		t.Fatal("19:09 tree dump / trailing 0.1.")
	}
	if !yamlTreeDump(liveRecap1909) {
		t.Fatal("19:09 yaml tree:")
	}
	if !trailingShortVersion(liveRecap1909) {
		t.Fatal("19:09 trailing 0.1.")
	}
	if !Truncated(liveRecap1915) {
		t.Fatal("19:15 leading e_test.go")
	}
	if !leadingFileFragment(liveRecap1915) {
		t.Fatal("19:15 file fragment")
	}
	if Truncated("Always never log Authorization headers in src/middleware/auth.ts.") {
		t.Fatal("Authorization truncated")
	}
	if Truncated("auth.ts Redis token bucket failed in staging.") {
		t.Fatal("auth.ts-first failed")
	}
	if Truncated("I'll stick with the parser that still extracts JWTs from cookies in src/auth.ts.") {
		t.Fatal("jwt still extracts truncated")
	}
	if Truncated("0.1.3 / 0.3 extract-clean: gate failed-work-first. Real pathful faileds stay.") {
		t.Fatal("0.1.3 remember truncated")
	}
	if Truncated(weightKeep) || trailingShortVersion(weightKeep) {
		t.Fatal("standing 4.0. truncated")
	}
	if Truncated("WShippedOverlap stays 2.5.") || trailingShortVersion("WShippedOverlap stays 2.5.") {
		t.Fatal("standing 2.5. truncated")
	}
	if Truncated(testGoFirstKeep) || leadingFileFragment(testGoFirstKeep) {
		t.Fatal("concurrent_test.go-first truncated")
	}
	if leadingFileFragment(namedLockKeep) || leadingFileFragment(fileLockKeep) {
		t.Fatal("named-lock keep as file fragment")
	}
}

func TestSkipProseLiveResidue(t *testing.T) {
	for _, s := range []string{
		"You can switch between them and never lose memory.",
		"you can switch between them and never lose your memory.",
		"Intended gap: Shipped channel is still 0.1.5: OpenCode plugin-miss, Codex desktop empty-rollout, and Claude unknown-cwd session files never reach the tape.",
		"Git objects are binary — I’ll run the actual git commands instead of inferring from object IDs.",
		"Same failure twice pauses as no-progress.",
		"The SQLITE_BUSY failure looks flaky; I'll rerun the full eval suite to confirm.",
		"0.1.7 extract-clean tree: slogans, I'll-run, intended-gap, same-failure-twice, process-state gated; Redis faileds and stick-with kept.",
		"Live prune superseded 8 noise rows (slogans, I'll-run, intended-gap, right-next-step, same-failure-twice).",
		"Tests failed on the live residue as expected.",
		"Ask warnings were treated as blocking for further product edits: do not dump an ask JSON body as assistant text; keep-tests (mobile SDK, failed worker, already-shipped, JWT still-extracts, 0.1.3 remember) stay.",
		`{"diff_stat":"13 files changed","ok":true,"summary":"0.1.7 gates slogans, I'll-run, intended-gap, same-failure-twice."}`,
		`{"diff_stat":"13 files changed, 322 insertions(+), 6 deletions(-)","ok":true,"summary":"Gate SkipProse now treats hyphenated I'll-run/intended-gap/same-failure-twice, live-residue recap, Ask JSON dumps, and test-lock narration as noise; Tests/Live/Ask are not identifiers.`,
		"Slogans, I'll-run, intended-gap, right-next-step, same-failure-twice, and the test-pass recap are gone.",
		"ProcessState is in SkipProse as planned; Working on … next is no longer a required kept state.",
		liveRecap1909,
		liveRecap1915,
		liveRecap1903,
		liveRecap1922,
		liveRecap1931,
		liveRecap1743,
		liveRecap1957,
		liveRecap2010,
		liveRecap2013,
		liveRecap2019,
	} {
		if !SkipProse(s) {
			t.Fatalf("residue not skipped: %q", s)
		}
	}
	for _, s := range []string{
		"Always never log Authorization headers in src/middleware/auth.ts.",
		"Redis token bucket failed in src/middleware/auth.ts staging.",
		"I'll stick with postgres instead of mysql.",
		"I'll cold-start at game size x 2 instead of resizing mid-session.",
		"0.1.3 / 0.3 extract-clean: gate failed-work-first. Real pathful faileds stay.",
		"slice-loop is autonomous.",
		"Same failure twice: Redis token bucket still 429 in src/middleware/auth.ts.",
		"Tests failed to connect after we raised the pool.",
		"I'll stick with the parser that still extracts JWTs from cookies in src/auth.ts.",
		namedLockKeep,
		fileLockKeep,
		testGoFirstKeep,
		weightKeep,
		theyFoundRedis,
		theyFoundLock,
		"They found Redis token bucket failed in this session in src/middleware/auth.ts.",
		"I'll stick with JWT next.",
		"We'll use postgres next.",
	} {
		if SkipProse(s) {
			t.Fatalf("lock skipped: %q", s)
		}
	}
	if !ProcessState("Working on billing invoices export next.") {
		t.Fatal("process-state leftover")
	}
	if SkipProse("Working on billing invoices export next.") {
		t.Fatal("process-state is type-scoped, not SkipProse")
	}
	if !ProcessState("A first run from the **lossless checkout**, with the example args, is the right next step.") {
		t.Fatal("right next step")
	}
	if SkipProse("A first run from the **lossless checkout**, with the example args, is the right next step.") {
		t.Fatal("right next step is type-scoped")
	}
	if !SkipProse("A They-found + Redis/path failed still stores and packs.") {
		t.Fatal("still stores and packs meta")
	}
	if !MetaFailedTalk(liveRecap1903) {
		t.Fatal("19:03 still store and pack")
	}
	if !MetaFailedTalk(liveRecap1922) {
		t.Fatal("19:22 recap-as-failed")
	}
	if !theyFoundReviewList(liveRecap1743) {
		t.Fatal("17:43 review-list shape")
	}
	if !InspectStatus(liveRecap1957) {
		t.Fatal("19:57 remaining-active recap")
	}
	if !InspectStatus(liveRecap2010) {
		t.Fatal("20:10 inspect window")
	}
	if !InspectStatus(liveRecap2013) {
		t.Fatal("20:13 inspect recent on")
	}
	if !InspectStatus(liveRecap2019) {
		t.Fatal("20:19 recap-faileds in window")
	}
	if InspectStatus(theyFoundRedis) || theyFoundReviewList(theyFoundRedis) {
		t.Fatal("they-found Redis as inspect-status")
	}
	if InspectStatus(theyFoundLock) || theyFoundReviewList(theyFoundLock) {
		t.Fatal("they-found named-lock as inspect-status")
	}
	if MetaFailedTalk(namedLockKeep) || MetaFailedTalk(fileLockKeep) || MetaFailedTalk(testGoFirstKeep) {
		t.Fatal("named-lock keep as meta-failed")
	}
	if MetaFailedTalk(theyFoundRedis) || MetaFailedTalk(theyFoundLock) {
		t.Fatal("they-found keep as meta-failed")
	}
}
