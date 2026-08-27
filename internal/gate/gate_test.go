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
	if !SkipProse("I'll match open zoom to the dismiss spring and find why the panel vanished.") {
		t.Fatal("i'll match planning")
	}
	if !SkipProse("I'll slow the open easeOut so it matches a swipe-down.") {
		t.Fatal("i'll slow planning")
	}
	if !SkipProse("I'll verify the generated output in a throwaway copy.") {
		t.Fatal("i'll verify planning")
	}
	if !SkipProse("I'll merge each one as it goes green instead of waiting for the whole batch.") {
		t.Fatal("i'll merge planning")
	}
	if !SkipProse("xcodebuild failed on the leftover xcresult; I'll clear it and restart.") {
		t.Fatal("i'll clear planning")
	}
	if !SkipProse("I'll measure the open animation on a 30fps sim.") {
		t.Fatal("i'll measure planning")
	}
	if SkipProse("I'll stick with postgres instead of mysql.") {
		t.Fatal("i'll stick with still a decision")
	}
	if SkipProse("I will clearly stick with JWT next.") {
		t.Fatal("i'll clearly is not i'll clear")
	}
	if SkipProse("I'll cleartext JWTs in src/auth.ts instead of hashing.") {
		t.Fatal("i'll cleartext is not i'll clear")
	}
	if SkipProse("Redis token bucket failed after lossless returned 429 in src/middleware/auth.ts.") {
		t.Fatal("lossless returned mid-sentence keep")
	}
	if !SkipProse("Lossless flags an old iOS open failure: the flyer sat behind the dest.") {
		t.Fatal("lossless flags meta")
	}
	if !SkipProse("Lossless returned prior-failure and constraint hits.") {
		t.Fatal("lossless returned pack echo")
	}
	if !SkipProse("Lossless flagged a prior overlay/Cancel failure.") {
		t.Fatal("lossless flagged pack echo")
	}
	if SkipProse("Redis token bucket failed after lossless flagged 429 in src/middleware/auth.ts.") {
		t.Fatal("lossless flagged mid-sentence keep")
	}
	if !SkipProse("I'll search Gitea for existing tickets and confirm root causes.") {
		t.Fatal("i'll search planning")
	}
	if !SkipProse("I'll map Favorites/Lightbox controls.") {
		t.Fatal("i'll map planning")
	}
	if !SkipProse("I'll call lossless first, then inspect the iOS evidence frames and the Home overlay/See-all code that failed live.") {
		t.Fatal("i'll call planning")
	}
	if !SkipProse("I'll shrink the Upload and AI-card diffs instead of raising the gate.") {
		t.Fatal("i'll shrink planning")
	}
	if !SkipProse("I'll point the live test at the cream failed tile, then run XCTest.") {
		t.Fatal("i'll point planning")
	}
	if !SkipProse("Let me also check what the previous failure on pipeline 1906 was about.") {
		t.Fatal("let me check planning")
	}
	if !SkipProse("Let me get the tail of the actual failure.") {
		t.Fatal("let me get planning")
	}
	if SkipProse("Let me ask lossless what's already decided.") {
		t.Fatal("let me ask keep")
	}
	if !SkipProse("That failed `agent-verify` is already fixed (ktlint, then re-run passed).") {
		t.Fatal("that failed agent-verify")
	}
	if !SkipProse("That failed `agent-verify` was the ktlint hit on #3382 — already fixed and merged.") {
		t.Fatal("that failed was the")
	}
	if !SkipProse("The earlier failed Gradle install is already superseded.") {
		t.Fatal("earlier failed superseded")
	}
	if !SkipProse("tially but restrict which steps receive it (remove from steps that don't need any Gitea API).") {
		t.Fatal("mid-word chop")
	}
	if SkipProse("env exists; do not print secret values") {
		t.Fatal("env exists keep")
	}
	if !SkipProse("Lossless will not abort a child if ask is missing.") {
		t.Fatal("lossless will pack echo")
	}
	if !SkipProse("Lossless ask returned the USB-only / cream-hole.") {
		t.Fatal("lossless ask returned pack echo")
	}
	if SkipProse("Redis token bucket failed after lossless will retry in src/middleware/auth.ts.") {
		t.Fatal("lossless will mid-sentence keep")
	}
	if !SkipProse("READ-ONLY: do not push, edit, or merge.") {
		t.Fatal("read-only instruction")
	}
	if !SkipProse("Return APPROVE or REQUEST_CHANGES with findings ranked by severity.") {
		t.Fatal("approve rubric")
	}
	if !SkipProse("Now I understand the failure.") {
		t.Fatal("now i understand")
	}
	if !SkipProse("The failed HomeView record is unrelated; I'll map Favorites/Lightbox controls.") {
		t.Fatal("failed record pack echo")
	}
	if !SkipProse("I'll open it next, then Upgrade and Log Out.") {
		t.Fatal("i'll open planning")
	}
	if !SkipProse("Prior failure was another surface, not Settings.") {
		t.Fatal("prior failure pack echo")
	}
	if !SkipProse(`","severity":"high","evidence":"/tmp/phone-qa/frames/f028.png"`) {
		t.Fatal("json severity shard")
	}
	if SkipProse("I'll stick with JWT next.") {
		t.Fatal("i'll stick with keep")
	}
	if SkipProse("I'll open-source the JWT limiter instead of forking.") {
		t.Fatal("i'll open-source keep")
	}
	if SkipProse("I will open-source the SDK instead of forking.") {
		t.Fatal("i will open-source keep")
	}
	if SkipProse("Redis token bucket failed after lossless flags were rewritten in src/middleware/auth.ts.") {
		t.Fatal("lossless flags mid-sentence keep")
	}
	if !SkipProse("Failed to tap Button (First Match): No matches found for Lightbox.") {
		t.Fatal("qa tap status")
	}
	if !SkipProse("lightbox_test.go:44: assertion failed") {
		t.Fatal("pasted go test")
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
	liveRecap1909     = "tree: productCopy slogans not bare never; space-form Same failure twice Redis still extracts; 0.1."
	liveRecap1915     = "e_test.go lock the recap row, not a pathful named-lock failed (contrast Redis token bucket)."
	liveRecap1903     = "Redis faileds, stick-with decisions, space-form “same failure twice” job-1, and pathless `Tests failed to` still store and pack."
	liveRecap1922     = "Shipping the current tree would lock fail-close skips and recap-as-failed."
	liveRecap1931     = "Ship only when inspect recent 8 are things a future session should obey and 19:09 is not a packed failed."
	liveRecap1743     = "They found real control-flow holes: budget headroom too small, a failed semver check aborting the whole batch, and a “shipped N” summary when the run just ran out of slots."
	liveRecap1957     = "One remaining active failed looks recap-like."
	liveRecap2010     = "Live recent 8 are slice-loop / 0.1.5 decisions and version state; `recent_noise=0`; 17:43 is not a packed failed."
	liveRecap2013     = "Inspect recent on the live store still includes the recap failed “Live recent 8 are slice-loop…”, which the uncommitted 0.1.7 gates already skip (`a packed failed` / `inspect recent 8`)."
	liveRecap2019     = "Live export still has recap-faileds in the recent window, and the tests gold yesterday’s skip phrases instead of proving that window is obey-worthy."
	namedLockKeep     = "Named locks in catchup.go stay on the session JSONL."
	fileLockKeep      = "File locks are tested in concurrent_test.go."
	testGoFirstKeep   = "concurrent_test.go-first File locks failed to acquire."
	weightKeep        = "WFailedOverlap stays 4.0."
	theyFoundRedis    = "They found Redis token bucket failed in src/middleware/auth.ts staging."
	theyFoundLock     = "They found the named-lock race in catchup.go."
	liveStillStores   = "A They-found Redis/path failed still stores."
	liveLoopResidue   = "Those recaps are loop residue; the product keep is: a real They-found Redis/path failed still stores."
	liveExampleDrop   = "Example drop: They found Redis token bucket failed in src/middleware/auth.ts staging."
	liveNeverLoseMemo = "Cross-harness, switch models, never lose memo"
	liveUnclosedParen = "Non-empty must not prune other projects (memora etc."
	exampleDropQuoted = `- "Example drop: They found Redis token bucket failed in src/middleware/auth.ts staging."`
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
	liveRecap2237gold  = "Gold Redis/named-lock/JWT/Tests-failed-to/concurrent_test."
	liveRecap2232keep  = "Locks still keep They-found Redis/path, named-lock, JWT next, Tests failed to, concurrent_test."
	liveRecap2227colon = "Locks still store: They-found Redis/path, named-lock, JWT next, Tests failed to connect, concurrent_test."
	liveRecap2227judge = "Recent 8 is not all obey-worthy (`A later session still checks out recap instead of work` plus slice-loop judge residue), so 0.3 stays open."
	liveRecap2217      = "A later session still checks out recap instead of work."
	liveRecap2243      = "They-found Redis, named-lock, I'll stick with JWT next, Tests failed to, and version.go keeps are not contains-skipped."
	// Live inspect --project jbgh/lossless after 0.1.9 gates (2026-08-19 22:55–22:56).
	liveRecap2256hyphen  = "They-found Redis, named-lock, JWT next, Tests failed to, version.go, and 4.0 still keep."
	liveRecap2255colon   = "Colon-form still-store lock-list recaps skip. A concurrent_test.go-first failed is not a go-first mash."
	liveRecap2255alone   = "Colon-form still-store lock-list recaps skip."
	liveRecapBenchGround = "Pathful Bench and Failed to still ground."
	stillKeepObject      = "Named locks still keep the session JSONL in catchup.go."
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
	if !Truncated(liveUnclosedParen) {
		t.Fatal("unclosed (memora etc.")
	}
	if Truncated("Redis token bucket failed (timeout).") {
		t.Fatal("closed (timeout) truncated")
	}
	if Truncated("WFailedOverlap stays 4.0.") {
		t.Fatal("standing 4.0. truncated")
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
		liveStillStores,
		liveExampleDrop,
		liveUnclosedParen,
		exampleDropQuoted,
		liveRecap2146,
		liveRecap2256hyphen,
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
		jwtNextKeep,
		"We'll use postgres next.",
		versionKeep,
		"Bench failed in testdata/bench/cases/01-auth.json.",
		"Bench failed to converge after we raised the batch.",
		stillKeepObject,
		"Login returned ok=false in src/auth.ts after we rotated tokens.",
		"This cut makes the Redis limiter fail in src/middleware/auth.ts.",
		"Never lose memoization of JWT claims in src/auth.ts.",
		"Inspect recapture of Redis sessions failed in src/watch.ts.",
		"We still store: the session JSONL on disk in catchup.go.",
		"The rate limit gates mostly match the Redis spec in src/middleware/auth.ts.",
		"Named locks still keep the session JSONL in catchup.go.",
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
	if !MetaFailedTalk(liveStillStores) || !stillExtractsNoObject(Fold(liveStillStores)) {
		t.Fatal("still stores. without and pack")
	}
	if MetaFailedTalk("Those recaps are loop residue and the product keep is a Redis failed.") {
		t.Fatal("loop residue phrase is not a skip")
	}
	if !exampleDrop(liveExampleDrop) || !exampleDrop(exampleDropQuoted) {
		t.Fatal("example drop after list-marker and quote trim")
	}
	if exampleDrop(theyFoundRedis) {
		t.Fatal("unprefixed they-found Redis as example drop")
	}
	if ProductCopy("Never lose memoization of JWT claims in src/auth.ts.") {
		t.Fatal("never lose memoization")
	}
	if MetaFailedTalk("Login returned ok=false in src/auth.ts after we rotated tokens.") {
		t.Fatal("ok=false health failed")
	}
	if MetaFailedTalk("This cut makes the Redis limiter fail in src/middleware/auth.ts.") {
		t.Fatal("this cut makes Redis fail")
	}
	if MetaFailedTalk("Inspect recapture of Redis sessions failed in src/watch.ts.") {
		t.Fatal("inspect recapture")
	}
	if MetaFailedTalk("The rate limit gates mostly match the Redis spec in src/middleware/auth.ts.") {
		t.Fatal("gates mostly match Redis spec")
	}
	if !MetaFailedTalk(liveRecap2146) {
		t.Fatal("go-first mash")
	}
	if MetaFailedTalk(liveRecap2227colon) {
		t.Fatal("colon still store: is a keep")
	}
	if !MetaFailedTalk(liveRecap2256hyphen) || !theyFoundHyphenList(liveRecap2256hyphen) {
		t.Fatal("22:56 hyphen they-found lock-list")
	}
	if theyFoundHyphenList(theyFoundRedis) || theyFoundHyphenList(theyFoundLock) {
		t.Fatal("space-form they-found as hyphen lock-list")
	}
	if stillExtractsNoObject(Fold(liveRecap2255alone)) {
		t.Fatal("hyphenated still-store as extract-meta")
	}
	if stillExtractsNoObject(Fold(liveRecapBenchGround)) {
		t.Fatal("still ground. as extract-meta")
	}
	if stillExtractsNoObject(Fold(stillKeepObject)) || MetaFailedTalk(stillKeepObject) {
		t.Fatal("still keep + object as no-object")
	}
	if stillExtractsNoObject(Fold(liveRecap2227colon)) || stillExtractsNoObject(Fold("We still store: the session JSONL on disk in catchup.go.")) {
		t.Fatal("colon still store: as no-object")
	}
	if stillExtractsNoObject(Fold("I'll stick with the parser that still extracts JWTs from cookies in src/auth.ts.")) {
		t.Fatal("still extracts JWTs as no-object")
	}
	if goFirstMash(Fold(testGoFirstKeep)) {
		t.Fatal("concurrent_test.go-first as go-first mash")
	}
	if !goFirstMash(Fold(liveRecap2146)) {
		t.Fatal("go-first mash missed")
	}
	if MetaFailedTalk(versionKeep) || MetaFailedTalk(jwtNextKeep) || MetaFailedTalk(weightKeep) {
		t.Fatal("version/jwt/4.0 keep as meta-failed")
	}
	if MetaFailedTalk(testGoFirstKeep) {
		t.Fatal("concurrent_test.go-first keep as meta-failed")
	}
	if MetaFailedTalk("ExtractNoise wrongly dropped the Redis failed in src/middleware/auth.ts.") {
		t.Fatal("extractnoise contains-skip")
	}
	if SkipProse("They found Redis token bucket failed in src/middleware/auth.ts staging.") {
		t.Fatal("they-found contains-skip")
	}
}
