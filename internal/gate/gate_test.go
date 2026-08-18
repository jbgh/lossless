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
