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
}
