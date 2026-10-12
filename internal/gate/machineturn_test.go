package gate

import "testing"

func TestMachineTurn(t *testing.T) {
	machine := []string{
		"",
		"/compact",
		"/reload-plugins now",
		"Task: Repo: /work/api (Go). You are in a managed git worktree.",
		"[Request interrupted by user]",
		"This session is being continued from a previous conversation.",
		"You are one of five parallel reviewers.",
		"2 background agents were stopped by the user: \"A\", \"B\".",
		`"Reply with exactly: OK"`,
		"▣ DCP | ~3.2K tokens removed, 14 tool outputs pruned",
		// two marks of a brief, at a brief's length: an absolute path and
		// what to hand back
		"Explore the app at /Users/dev/app/worktrees/x thoroughly. I need to understand how images are loaded, " +
			"cached and decoded across the gallery and the viewer, and which screens still use the old loader. " +
			"Report back the files that load images, with line numbers.",
		// capitals and a list
		"Independent QA. Do NOT fix code. 1. open the frames in the capture folder and step through the fling " +
			"2. compare them against the reference timings, noting each dropped frame 3. write up what differs, " +
			"with the frame numbers, and nothing else",
	}
	for _, s := range machine {
		if !MachineTurn(s) {
			t.Errorf("machine turn kept: %q", s)
		}
	}
	human := []string{
		"lets use the smaller model for all subagents since its pretty good now",
		"You are right, lets keep the old layout.",
		"/Users/me/Downloads/edits.pdf also lets review this document again",
		"Don't look at anything except the beach level",
		"I think the buttons for the languages could be better, let's add flags",
		"can you give me multiple choice with recommendations?",
		// short and emphatic is a person, whatever marks it carries
		"getUser should return the cached row, NOT refetch",
		"do NOT touch /Users/me/app/config.yaml",
		"@internal/retrieve/ask.go keep the floor off for HEAD asks",
		"[Image #1] make the header sticky like this, on every page",
		"/tmp is full again, clean it up before the next build",
		"<Button> should be bigger on the checkout page",
		`"Save draft" should read "Save as draft"`,
		"Your task list is wrong, always check docs/plan.md first",
		"you are in the wrong directory, always cd to app/ first",
	}
	for _, s := range human {
		if MachineTurn(s) {
			t.Errorf("human turn dropped: %q", s)
		}
	}
}

func TestBriefSignals(t *testing.T) {
	if n := BriefSignals("lets keep the export columns in the order people rely on"); n != 0 {
		t.Fatalf("plain turn has %d brief signals", n)
	}
	if n := BriefSignals("Read /Users/dev/app/plan.md. Do NOT edit. Return a list of gaps."); n < 3 {
		t.Fatalf("brief has %d signals, want 3", n)
	}
}
