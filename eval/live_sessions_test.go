package eval

import (
	"os"
	"strings"
	"testing"

	"lossless/internal/env"
	"lossless/internal/retrieve"
	"lossless/internal/store"
)

// Live sessions on this machine. Skip unless LOSSLESS_LIVE=1 so CI
// does not depend on ~/.lossless.
func TestLiveSessionsAskQuality(t *testing.T) {
	if os.Getenv("LOSSLESS_LIVE") != "1" {
		t.Skip("set LOSSLESS_LIVE=1 to score retrieve against this machine's Grok sessions")
	}
	home := env.Home()
	st, err := store.Open(home)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	cases := []retrieve.Request{
		{
			WorkspaceRoot: "/Users/jaybyoun/developer/lossless",
			SessionID:     "01a003db-f4a6-7f43-a694-082428bbff32",
			Goal:          "improve retrieve using live sessions as tests",
			Question:      "what must I not forget about extract and ask context",
			Paths:         []string{"internal/retrieve/ask.go", "internal/write/extract.go"},
		},
		{
			WorkspaceRoot: "/Users/jaybyoun/developer/memora-v2",
			SessionID:     "019ffbda-a0f1-7ea2-b21a-23f65798ecec",
			Goal:          "fix iOS lightbox swipe-down dismiss hero",
			Question:      "what already failed or what did we decide about lightbox",
		},
		{
			WorkspaceRoot: "/Users/jaybyoun/developer/memora-v2",
			Goal:          "run android lightbox QA on the emulator",
			Question:      "what constraints apply to android emulator testing",
		},
		{
			WorkspaceRoot: "/Users/jaybyoun/developer/country-club-empire",
			SessionID:     "019ff929-d8c3-7741-a64a-2be218bde5b2",
			Goal:          "continue the current feature work",
			Question:      "what must I not forget",
		},
		{
			WorkspaceRoot: "/Users/jaybyoun/developer/coffee-meats-gym",
			SessionID:     "01a00bbb-c54a-7f83-a7fa-13277dcf306a",
			Goal:          "continue the current feature work",
			Question:      "what must I not forget",
		},
		{
			WorkspaceRoot: "/Users/jaybyoun/developer/agent-memory",
			SessionID:     "01a002a6-6672-7a53-ad36-e68ce5ab7954",
			Goal:          "continue memory work",
			Question:      "what did we decide",
		},
	}

	var failed int
	for _, req := range cases {
		req := req
		name := req.WorkspaceRoot
		if i := strings.LastIndex(name, "/"); i >= 0 {
			name = name[i+1:]
		}
		if req.Goal != "" {
			name += "/" + strings.Split(req.Goal, " ")[0]
		}
		t.Run(name, func(t *testing.T) {
			out, err := retrieve.Ask(st, req)
			if err != nil {
				t.Fatal(err)
			}
			nFail := 0
			for _, h := range out.Context {
				if h.Type == "failed" {
					nFail++
				}
				if noise := liveNoise(h); noise != "" {
					t.Errorf("%s: %s", noise, h.Text)
					failed++
				}
			}
			if nFail > retrieve.PackTypeCap {
				t.Errorf("failed flood %d", nFail)
				failed++
			}
			t.Logf("project=%s hits=%d warns=%d", out.Project, len(out.Context), len(out.Warnings))
			for i, h := range out.Context {
				t.Logf("  %d [%s] %s", i+1, h.Type, strings.ReplaceAll(h.Text, "\n", " "))
			}
		})
	}
	if failed > 0 {
		t.Fatalf("%d live quality issues", failed)
	}
}

func liveNoise(h retrieve.Hit) string {
	t := strings.TrimSpace(h.Text)
	low := strings.ToLower(t)
	if strings.HasPrefix(t, "#") {
		return "heading"
	}
	if strings.HasPrefix(t, "|") || strings.Contains(t, " | ") {
		return "table"
	}
	if strings.HasPrefix(t, "- ") && len(t) < 80 {
		return "short-bullet"
	}
	if h.Type == "constraint" && (strings.Contains(low, "why don't you") || strings.Contains(low, "don't ask") || strings.Contains(low, "don't change source")) {
		return "session-op"
	}
	if h.Type == "decision" && (strings.Contains(low, "i'll start") || strings.Contains(low, "i’ll start") || strings.Contains(low, "i'll check") || strings.Contains(low, "i’ll check") || strings.Contains(low, "i'll fix") || strings.Contains(low, "i’ll fix")) {
		return "planning"
	}
	if strings.Contains(low, "failure mode") {
		return "meta"
	}
	if strings.HasSuffix(t, "(") || strings.HasSuffix(t, "`." ) || strings.HasSuffix(t, "do not") {
		return "truncated"
	}
	if strings.Contains(low, "failed-overlap") || strings.Contains(low, "classified as") {
		return "meta"
	}
	if h.Type == "failed" && (strings.Contains(low, "ci unit-test") || strings.Contains(low, "unit-test failure") || strings.Contains(low, "checking #") || strings.Contains(low, "which of those") || strings.Contains(low, "background notification") || strings.Contains(low, "re-pushing")) {
		return "status-failed"
	}
	if h.Type == "state" && (strings.Contains(low, "next test that matters") || strings.Contains(low, "in this session") || strings.Contains(low, "that row is always there") || strings.Contains(low, "i'll inspect") || strings.Contains(low, "i’ll inspect")) {
		return "process-state"
	}
	return ""
}
