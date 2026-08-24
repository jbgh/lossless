package retrieve

import (
	"strings"
	"testing"

	"lossless/internal/claim"
	"lossless/internal/gate"
	"lossless/internal/redact"
)

func TestAdv191ExtractNoiseDropsLiveMemoraChrome(t *testing.T) {
	drops := []claim.Record{
		{Type: "constraint", Text: "READ-ONLY: do not push, edit, or merge."},
		{Type: "constraint", Text: "READ-ONLY: do not edit/push/merge."},
		{Type: "failed", Text: "Now I understand the failure.", Paths: []string{"src/middleware/auth.ts"}},
		{Type: "failed", Text: "Return APPROVE or REQUEST_CHANGES with findings ranked by severity + concrete failure scenario."},
		{Type: "failed", Text: "RETURN a ranked findings report: title, severity, file:line, failure scenario."},
		{Type: "failed", Text: "Lossless will not abort a child if ask is missing."},
		{Type: "failed", Text: "Lossless ask returned the USB-only / cream-hole."},
		{Type: "decision", Text: `","severity":"high","evidence":"/tmp/phone-qa-2026-08-24/child-milestones/frames/f028.png"`},
	}
	for _, rec := range drops {
		if !gate.SkipProse(rec.Text) && !extractNoise(rec) {
			t.Fatalf("would still pack %s %q", rec.Type, rec.Text)
		}
		if !extractNoise(rec) {
			t.Fatalf("extractNoise miss %s %q", rec.Type, rec.Text)
		}
	}
	keep := claim.Record{Type: "failed", Text: "Redis token bucket failed in staging."}
	if extractNoise(keep) {
		t.Fatal("pathless Redis dropped")
	}
	if extractNoise(claim.Record{Type: "decision", Text: "I'll stick with JWT next."}) {
		t.Fatal("i'll stick with dropped")
	}
}

func TestAdv191AskPathsDropTmpNotSrcTmp(t *testing.T) {
	q, err := normalize(Request{
		Project:  "acme/api",
		Question: "what failed",
		Goal:     "fix limiter",
		Paths: []string{
			"/tmp/phone-qa/qa-report.md",
			"tmp/phone-qa/f028.png",
			"qa-report.md",
			"src/tmp/limiter.ts",
			"src/middleware/auth.ts",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(q.PathKeys, " ")
	if strings.Contains(joined, "f028") || hasExact(q.PathKeys, "qa-report.md") {
		t.Fatalf("scratch leaked: %v", q.PathKeys)
	}
	if !strings.Contains(joined, "limiter") || !strings.Contains(joined, "auth") {
		t.Fatalf("kept paths missing: %v", q.PathKeys)
	}
	q2, err := normalize(Request{
		Project:  "acme/api",
		Question: "what failed",
		Goal:     "fix limiter",
		Paths:    []string{"docs/qa-report.md"},
	})
	if err != nil || !strings.Contains(strings.Join(q2.PathKeys, " "), "docs/qa-report.md") {
		t.Fatalf("docs/qa-report.md dropped: %v %v", q2.PathKeys, err)
	}
	got := redact.FilterPaths([]string{"src/tmp/limiter.ts", "docs/qa-report.md", "qa-report.md", "tmp/x.png"})
	if len(got) != 2 || got[0] != "src/tmp/limiter.ts" || got[1] != "docs/qa-report.md" {
		t.Fatalf("%v", got)
	}
}

func TestAdv191OnlyAbsScratchDoesNotLeakBasename(t *testing.T) {
	q, err := normalize(Request{
		Project:  "acme/api",
		Question: "what failed",
		Goal:     "fix limiter",
		Paths:    []string{"/tmp/phone-qa/qa-report.md"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if hasExact(q.PathKeys, "qa-report.md") {
		t.Fatalf("basename leaked from only scratch path: %v", q.PathKeys)
	}
	for _, k := range q.PathKeys {
		low := strings.ToLower(k)
		if strings.Contains(low, "qa-report") || strings.Contains(low, "phone-qa") {
			t.Fatalf("scratch leaked: %v", q.PathKeys)
		}
	}
}

func hasExact(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}

func TestAdv191LiveMemoraStoreChrome(t *testing.T) {
	drops := []claim.Record{
		{Type: "constraint", Text: `READ-ONLY: do not push, edit, or merge.`},
		{Type: "failed", Text: `Now I understand the failure.`, Paths: []string{"web/next.config.ts", "ios/Memora/Preview/MockURLProtocol.swift", "backend/internal/services/share_link.go"}},
		{Type: "constraint", Text: `READ-ONLY — do not edit, commit, push, or post anything to Gitea.`},
		{Type: "constraint", Text: `READ-ONLY: do not edit/push/merge.`},
		{Type: "constraint", Text: `READ-ONLY AUDIT — do NOT modify any file, do NOT open PRs.`},
		{Type: "failed", Text: `RETURN a ranked findings report: title, severity, file:line, failure scenario, agent-doable vs Jay-gated.`},
		{Type: "constraint", Text: `READ-ONLY — do not edit, commit, push, or post anything to Gitea (read-only API GETs are fine).`},
		{Type: "failed", Text: `Return APPROVE or REQUEST_CHANGES, findings ranked by severity + failure scenario.`},
		{Type: "failed", Text: `Return APPROVE or REQUEST_CHANGES with findings + failure scenario.`},
		{Type: "failed", Text: `Return APPROVE or REQUEST_CHANGES with findings ranked by severity + concrete failure scenario.`},
		{Type: "failed", Text: `Return APPROVE or REQUEST_CHANGES per-change, findings ranked + failure scenario.`},
		{Type: "failed", Text: `Rank any findings by severity with file:line + failure scenario.`},
		{Type: "constraint", Text: `READ-ONLY AUDIT — do NOT modify any file, do NOT change any server, do NOT open PRs.`},
		{Type: "failed", Text: `Return a verdict: APPROVE or REQUEST_CHANGES, with any findings ranked by severity (file:line + concrete failure scenario).`},
		{Type: "failed", Text: `For each: title, severity (Critical/High/Med/Low), file:line, concrete failure scenario, and whether it's **agent-doable now** or **Jay-gated/needs-decision**.`},
		{Type: "failed", Text: `RETURN a ranked findings report (highest-leverage first): title, severity, file:line, failure scenario, agent-doable vs Jay-gated.`},
		{Type: "constraint", Text: `READ-ONLY: do not edit, push, or merge.`},
		{Type: "constraint", Text: `You are doing a quick competitive web research task (READ-ONLY, web only — do not touch the repo).`},
		{Type: "failed", Text: `Return APPROVE or REQUEST_CHANGES with findings ranked by severity (file:line + concrete failure scenario).`},
		{Type: "constraint", Text: `Read-only: do NOT edit files, do NOT commit.`},
		{Type: "decision", Text: `","severity":"high","evidence":"/tmp/phone-qa-2026-08-24/child-milestones/frames/f028.png → f029.png vs /tmp/phone-qa-2026-08-24/child-milestones/select/00-qa-child-earlier.png"},{"issue":"Settings → Children during family 429 opens Family “Couldn't load your families” (wrong screen) instead of the child list.`},
		{Type: "failed", Text: "Lossless will not abort a child if `ask` is missing."},
		{Type: "failed", Text: `Lossless ask returned the USB-only / cream-hole / keep-dest-until-Home / restore-scales-to-1.0 constraints; the two failed records were unrelated (lerp width, Albums PTR).`},
		{Type: "decision", Text: `","severity":"medium","evidence":"/tmp/android-qa/family-children/frames/041.png"},{"issue":"Back from the dedicated Children list resets Family to the Members tab instead of restoring the Children tab.`},
		{Type: "decision", Text: `","severity":"low","evidence":"/tmp/android-qa/map/frames/f013.png"},{"issue":"SF pin opens a video lightbox (Beach day video) instead of a photo; PreviewMode canned locations bind to the first two media items, both videos.`},
		{Type: "decision", Text: `","severity":"high","evidence":"/tmp/android-qa/person-detail/frames/10-people-list.png vs /tmp/android-qa/person-detail/frames/11-emma-profile.png and /tmp/android-qa/person-detail/frames/16-mike-profile.png"},{"issue":"Edit-name control on an already-named person (Emma) opens a dialog titled 'Add name' instead of edit/rename copy.`},
		{Type: "failed", Text: `","severity":"medium","evidence":"/tmp/phone-qa/lightbox-video/sample/f072-home-video-tile.png"},{"issue":"Mute works visually (speaker.wave → speaker.slash at 0:09) but Unmute XCUITest tap failed: button identity is the SF Symbol (speaker.slash.fill) so 'Unmute' IN identifiers matched nothing.`},
	}
	for _, rec := range drops {
		if !gate.SkipProse(rec.Text) && !extractNoise(rec) {
			t.Errorf("would still pack %s %q", rec.Type, rec.Text)
		}
	}
	if t.Failed() {
		t.Fatal("live memora chrome still packs")
	}
	if extractNoise(claim.Record{Type: "failed", Text: "Redis token bucket failed in staging."}) {
		t.Fatal("pathless Redis dropped")
	}
	if extractNoise(claim.Record{Type: "decision", Text: "I'll stick with JWT next."}) {
		t.Fatal("I'll stick with JWT dropped")
	}
}
