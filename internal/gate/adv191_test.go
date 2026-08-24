package gate

import "testing"

func TestAdv191PackEchoAndInstructionChrome(t *testing.T) {
	skip := []string{
		"I'll map Favorites/Lightbox controls.",
		"Lossless will not abort a child if ask is missing.",
		"Lossless ask returned the USB-only / cream-hole.",
		"Lossless flagged a prior overlay/Cancel failure.",
		"READ-ONLY: do not push, edit, or merge.",
		"Return APPROVE or REQUEST_CHANGES with findings ranked by severity.",
		"Now I understand the failure.",
	}
	for _, s := range skip {
		if !SkipProse(s) {
			t.Fatalf("kept %q", s)
		}
	}
	keep := []string{
		"Redis token bucket failed after lossless will retry in src/middleware/auth.ts.",
		"I'll stick with JWT next.",
		"I'll open-source the JWT limiter instead of forking.",
		"Redis token bucket failed in src/middleware/auth.ts staging.",
	}
	for _, s := range keep {
		if SkipProse(s) {
			t.Fatalf("skipped %q", s)
		}
	}
}

func TestAdv191InstructionChromeQuotedBoldVerdict(t *testing.T) {
	skip := []string{
		`"READ-ONLY: do not push, edit, or merge."`,
		`**READ-ONLY: do not push, edit, or merge.**`,
		"“READ-ONLY: do not push, edit, or merge.”",
		"Return a verdict: APPROVE or REQUEST_CHANGES with findings ranked by severity.",
		"Return a verdict: APPROVE or REQUEST_CHANGES…",
	}
	for _, s := range skip {
		s := s
		t.Run("skip/"+s, func(t *testing.T) {
			if !InstructionChrome(s) {
				t.Errorf("InstructionChrome kept %q", s)
			}
			if !SkipProse(s) {
				t.Errorf("SkipProse kept %q", s)
			}
		})
	}
	keep := []string{
		"Redis token bucket failed after lossless will retry in src/middleware/auth.ts.",
		"Redis token bucket failed in staging.",
		"Redis token bucket failed.",
	}
	for _, s := range keep {
		s := s
		t.Run("keep/"+s, func(t *testing.T) {
			if InstructionChrome(s) {
				t.Errorf("InstructionChrome dropped keep %q", s)
			}
			if SkipProse(s) {
				t.Errorf("SkipProse dropped keep %q", s)
			}
		})
	}
}
