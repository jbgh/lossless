package write

import (
	"strings"
	"testing"
)

// A decision must name a real referent: a path, a tick or bold span, a
// code-shaped token, a mid-sentence proper noun, an acronym, or a
// structural use-X-not-Y / picked-X-over-Y shape. Bare commitment verbs
// with no object ("I'll stick with keep.") are narration.
func TestGroundedDecisionAnchors(t *testing.T) {
	yes := []string{
		"Use jose, not jsonwebtoken, for Edge.",
		"I'll stick with JWT next.",
		"Decided to keep the codex parse in parse_codex.go.",
		"Picked pgx over lib/pq for the pool.",
		"We'll use the token bucket in `limiter.go`.",
		"Going with Postgres for the queue.",
	}
	for _, s := range yes {
		if !GroundedDecision(s, nil) {
			t.Errorf("GroundedDecision(%q) = false, want true", s)
		}
	}
	no := []string{
		"I'll stick with keep.",
		"Before I lay out the list, let me ground it in what's already decided or on the roadmap so I'm not re-proposing things you've rejected.",
		"We decided to keep going.",
	}
	for _, s := range no {
		if GroundedDecision(s, nil) {
			t.Errorf("GroundedDecision(%q) = true, want false", s)
		}
	}
	if !GroundedDecision("We decided to keep going.", []string{"src/app.ts"}) {
		t.Error("caller paths must ground a decision")
	}
}

func TestExtractDropsUngroundedDecisionsAndArrowChrome(t *testing.T) {
	msgs := []Message{
		{Role: "assistant", Text: "I'll stick with keep.", Offset: 1},
		{Role: "assistant", Text: "Before I lay out the list, let me ground it in what's already decided or on the roadmap so I'm not re-proposing things you've rejected.", Offset: 2},
		{Role: "assistant", Text: "Production code → test exists and failed first.", Offset: 3},
		{Role: "assistant", Text: "We decided to use jose, not jsonwebtoken, for Edge.", Offset: 4},
		{Role: "assistant", Text: "Renamed parse.go → parse_codex.go and the build failed on the old import.", Offset: 5},
	}
	got := Extract(msgs, ExtractOpts{ProjectKey: "acme/api", Harness: "grok", SessionID: "s", Source: "turn"})
	var joseOK, prSizeOK bool
	for _, r := range got {
		switch {
		case strings.Contains(r.Text, "stick with keep"):
			t.Fatalf("objectless decision stored: %+v", r)
		case strings.Contains(r.Text, "lay out the list"):
			t.Fatalf("planning narration stored: %+v", r)
		case strings.Contains(r.Text, "Production code"):
			t.Fatalf("arrow chrome stored: %+v", r)
		case strings.Contains(r.Text, "jose"):
			joseOK = r.Type == "decision"
		case strings.Contains(r.Text, "parse_codex"):
			prSizeOK = r.Type == "failed"
		}
	}
	if !joseOK {
		t.Fatalf("grounded jose decision missing: %+v", got)
	}
	if !prSizeOK {
		t.Fatalf("pathful arrow failed missing: %+v", got)
	}
}

func TestGroundedDecisionRejectsDigitsPronounsAbbrev(t *testing.T) {
	no := []string{
		"Decided to go that way instead of the other.",
		"We decided to wait 2 weeks before retrying.",
		"Decided to go with the simpler path, e.g. skipping the cache.",
	}
	for _, s := range no {
		if GroundedDecision(s, nil) {
			t.Errorf("GroundedDecision(%q) = true, want false", s)
		}
	}
	if !GroundedDecision("we're standardizing on react-query for server state", nil) {
		t.Error("kebab package name must ground")
	}
	if !GroundedDecision("Use httpx, not requests, and pin v0.27.", nil) {
		t.Error("version token must still ground")
	}
}

// A workflow finding is a failed even when phrased with a unicode arrow.
func TestExtractKeepsArrowWorkflowFindings(t *testing.T) {
	body := `{"asked":true,"findings":[{"issue":"Checkout webhook: expected 200 → got 500 on Stripe retry.","severity":"high"}],"ok":true}`
	got := Extract([]Message{{Role: "assistant", Text: body, Offset: 1}},
		ExtractOpts{ProjectKey: "acme/api", Harness: "grok", SessionID: "s", Source: "turn"})
	found := false
	for _, r := range got {
		if r.Type == "failed" && strings.Contains(r.Text, "expected 200") {
			found = true
		}
	}
	if !found {
		t.Fatalf("arrow finding lost: %+v", got)
	}
}

// A decision grounded by a neighbor sentence's path must carry that path
// so the read-time gate can retrieve it.
func TestNeighborGroundedDecisionAttachesPath(t *testing.T) {
	got := Extract([]Message{{
		Role: "assistant", Offset: 1,
		Text: "We decided to keep the limiter. The details are in src/limiter.go.",
	}}, ExtractOpts{ProjectKey: "acme/api", Harness: "grok", SessionID: "s", Source: "turn"})
	for _, r := range got {
		if strings.Contains(r.Text, "keep the limiter") {
			if len(r.Paths) == 0 {
				t.Fatalf("neighbor-grounded decision stored pathless: %+v", r)
			}
			return
		}
	}
	t.Fatalf("neighbor-grounded decision missing: %+v", got)
}
