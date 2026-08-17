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
}
