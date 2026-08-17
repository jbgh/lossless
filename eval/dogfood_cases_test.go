package eval

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"lossless/internal/claim"
	"lossless/internal/redact"
	"lossless/internal/retrieve"
	"lossless/internal/write"
)

func testMetaCommentary(t *testing.T) {
	st := dogfoodStore(t)
	var b strings.Builder
	b.WriteString(grokLine("assistant", "Who-reacted failed in preview in ios/LightboxView.swift."))
	b.WriteString(grokLine("assistant", "Ask pack only has the shrink-cursor failure and the 5s SQLite busy timeout."))
	b.WriteString(grokLine("assistant", "Raising to 8 or 10 would make recall look better — extract noise classified as `failed`."))
	b.WriteString(grokLine("assistant", "Force in the best failed-overlap (do not repeat burned work)."))
	b.WriteString(grokLine("assistant", "I'll pull the retrieve decisions, then separate what failed: extract, grounding, or the packer."))
	b.WriteString(grokLine("assistant", "The live ask just returned five `failed`s, and four look like extract noise."))
	dogfoodCatch(t, st, "acme/app", "meta", b.String())

	out := askAtNow(t, st, retrieve.Request{
		Project: "acme/app", Goal: "fix lightbox who-reacted",
		Question: "what already failed about retrieve extract and lightbox",
		Paths:    []string{"ios/LightboxView.swift"},
	})
	if !contextHas(out, "Who-reacted") {
		t.Fatalf("gold missed: %+v", out.Context)
	}
	if contextHasAny(out, "Ask pack only", "classified as", "failed-overlap", "I'll pull", "five `failed`") {
		t.Fatalf("meta drowned gold: %+v", out.Context)
	}
}

func testMarkdownChrome(t *testing.T) {
	st := dogfoodStore(t)
	var b strings.Builder
	b.WriteString(grokLine("assistant", "We decided to use jose, not jsonwebtoken, for Edge in src/middleware/auth.ts."))
	b.WriteString(grokLine("assistant", "## Investigation: why those uploads failed"))
	b.WriteString(grokLine("assistant", "| **Claims** | owner/repo | A Grok `failed` on acme/api is what Claude’s ask is supposed to see."))
	b.WriteString(grokLine("assistant", "- Redis limiter **failed** (twice) + warning: do not repeat"))
	b.WriteString(grokLine("assistant", "**What you do next**"))
	b.WriteString(grokLine("assistant", "> Redis token bucket failed in staging."))
	dogfoodCatch(t, st, "acme/app", "md", b.String())

	out := askAtNow(t, st, retrieve.Request{
		Project: "acme/app", Question: "why not jsonwebtoken",
		Paths: []string{"src/middleware/auth.ts"},
	})
	if !contextHas(out, "jose") {
		t.Fatalf("decision missed: %+v", out.Context)
	}
	for _, h := range out.Context {
		if strings.HasPrefix(strings.TrimSpace(h.Text), "#") || strings.HasPrefix(h.Text, "|") || strings.Contains(h.Text, "What you do next") {
			t.Fatalf("chrome packed: %+v", h)
		}
	}
}

func testAgentPrompts(t *testing.T) {
	st := dogfoodStore(t)
	var b strings.Builder
	b.WriteString(grokLine("user", "Always never log Authorization headers in src/middleware/auth.ts."))
	b.WriteString(grokLine("user", "why don't you pull up the simulator on an iphone 12 mini and see what it looks like"))
	b.WriteString(grokLine("user", "i'll be stepping away so don't ask any questions just do what you think is correct."))
	b.WriteString(grokLine("user", "- Don't change source"))
	b.WriteString(grokLine("user", "- Don't delete data"))
	b.WriteString(grokLine("user", "the scrubbing video doesn't seem to work why don't you verify on the emulator"))
	dogfoodCatch(t, st, "acme/app", "prompt", b.String())

	out := askAtNow(t, st, retrieve.Request{
		Project: "acme/app", Question: "Authorization headers",
		Paths: []string{"src/middleware/auth.ts"},
	})
	if !contextHas(out, "Authorization") {
		t.Fatalf("real constraint missed: %+v", out.Context)
	}
	if contextHasAny(out, "why don't you", "Don't change source", "don't ask", "Don't delete") {
		t.Fatalf("session ops packed: %+v", out.Context)
	}
}

func testPlanning(t *testing.T) {
	st := dogfoodStore(t)
	var b strings.Builder
	b.WriteString(grokLine("assistant", "We decided to use jose, not jsonwebtoken, for Edge in src/middleware/auth.ts."))
	b.WriteString(grokLine("assistant", "I'll check what we already decided, then install a real Grok/Claude skill as part of the product."))
	b.WriteString(grokLine("assistant", "I'll fix extract, cap failed eviction, and add tests from these real sessions."))
	b.WriteString(grokLine("assistant", "This is a product concept, so I'll start with the brainstorming and office-hours process instead of jumping straight to slides."))
	b.WriteString(grokLine("assistant", "I'll inspect both accounts' current plan and Stripe fields first."))
	b.WriteString(grokLine("assistant", "I'll cold-start at game size x 2 instead of resizing mid-session."))
	dogfoodCatch(t, st, "acme/app", "plan", b.String())

	out := askAtNow(t, st, retrieve.Request{
		Project: "acme/app", Question: "why not jsonwebtoken",
		Paths: []string{"src/middleware/auth.ts"},
	})
	if !contextHas(out, "jose") {
		t.Fatalf("jose missed: %+v", out.Context)
	}
	if contextHasAny(out, "I'll check", "I'll fix", "I'll start", "I'll inspect") {
		t.Fatalf("planning packed: %+v", out.Context)
	}
	// cold-start is a real product decision (not I'll check/start with)
	if !contextHas(out, "cold-start") && !contextHas(out, "jose") {
		t.Fatal("expected a real decision")
	}
}

func testCIStatus(t *testing.T) {
	st := dogfoodStore(t)
	var b strings.Builder
	b.WriteString(grokLine("assistant", "**Upload Complete** sheet: **7 of 10 uploaded, 3 failed**, each with **Could not load this photo**, and a Retry that does nothing."))
	for i := 3000; i < 3040; i++ {
		b.WriteString(grokLine("assistant", fmt.Sprintf("Checking #%d failure and re-pushing both.", i)))
	}
	b.WriteString(grokLine("assistant", "That background notification is just the local Android MediaUrlsTest run finishing (exit 0) from earlier while we fixed the CI unit-test failure."))
	b.WriteString(grokLine("assistant", "If anything still looks off on device after pull-to-refresh / reinstall, say which of those four failed."))
	b.WriteString(grokLine("assistant", "**pr-size-check** failed at **952 lines** → added audit-bundle"))
	dogfoodCatch(t, st, "acme/app", "ci", b.String())

	out := askAtNow(t, st, retrieve.Request{
		Project:  "acme/app",
		Goal:     "fix upload retry on lightbox",
		Question: "what already failed about uploads",
	})
	if !contextHas(out, "Upload Complete") {
		t.Fatalf("upload gold missed: %+v", out.Context)
	}
	if contextHasAny(out, "Checking #", "background notification", "CI unit-test", "which of those", "pr-size-check") {
		t.Fatalf("CI status packed: %+v", out.Context)
	}
	if countType(out, "failed") > retrieve.PackTypeCap {
		t.Fatalf("failed flood %d: %+v", countType(out, "failed"), out.Context)
	}
}

func testGitURLPath(t *testing.T) {
	st := dogfoodStore(t)
	var b strings.Builder
	b.WriteString(grokLine("assistant", "Checking #3081 failure and re-pushing both to git.memora.pics/memora/memora.git"))
	b.WriteString(grokLine("assistant", "Who-reacted failed in preview in ios/LightboxView.swift."))
	dogfoodCatch(t, st, "acme/app", "giturl", b.String())

	claims, _ := st.ListActive("acme/app")
	for _, c := range claims {
		for _, p := range c.Paths {
			if strings.Contains(p, ".git") || strings.Contains(p, "memora.pics") {
				t.Fatalf("git URL stored as path: %v in %s", c.Paths, c.Text)
			}
		}
	}
	out := askAtNow(t, st, retrieve.Request{
		Project: "acme/app", Question: "what failed",
		Paths: []string{"ios/LightboxView.swift"},
	})
	if contextHas(out, "Checking #3081") {
		t.Fatalf("git-grounded CI failed packed: %+v", out.Context)
	}
	if !contextHas(out, "Who-reacted") {
		t.Fatalf("product failed missed: %+v", out.Context)
	}
}

func testStrippedAbs(t *testing.T) {
	st := dogfoodStore(t)
	var b strings.Builder
	b.WriteString(grokLine("assistant", "We decided to keep the limiter in Users/jaybyoun/developer/lossless/docs/stack.md instead of Redis."))
	b.WriteString(grokLine("assistant", "Never commit keys/id_ed25519 or authorized_keys to the repo."))
	b.WriteString(grokLine("assistant", "We decided to use jose, not jsonwebtoken, for Edge in src/middleware/auth.ts."))
	dogfoodCatch(t, st, "acme/app", "abs", b.String())

	claims, _ := st.ListActive("acme/app")
	for _, c := range claims {
		got := redact.FilterPaths(c.Paths)
		for _, p := range got {
			if strings.HasPrefix(p, "Users/") || strings.Contains(p, "id_ed25519") || strings.Contains(p, "authorized_keys") {
				t.Fatalf("unsafe path kept: %v text=%s", c.Paths, c.Text)
			}
		}
	}
	out := askAtNow(t, st, retrieve.Request{
		Project: "acme/app", Question: "jose",
		Paths: []string{"src/middleware/auth.ts"},
	})
	if !contextHas(out, "jose") {
		t.Fatalf("jose missed: %+v", out.Context)
	}
}

func testFailedFlood(t *testing.T) {
	st := dogfoodStore(t)
	var b strings.Builder
	b.WriteString(grokLine("assistant", "Redis token bucket failed in src/middleware/auth.ts staging."))
	b.WriteString(grokLine("assistant", "We decided to use jose, not jsonwebtoken, for Edge in src/middleware/auth.ts."))
	for i := 0; i < 25; i++ {
		b.WriteString(grokLine("assistant", fmt.Sprintf("Helper decoy %d failed in src/other/file%d.ts during compile.", i, i)))
	}
	dogfoodCatch(t, st, "acme/app", "flood", b.String())

	out := askAtNow(t, st, retrieve.Request{
		Project: "acme/app", Goal: "add rate limiting",
		Question: "what failed about retrieve extract and the limiter",
		Paths:    []string{"src/middleware/auth.ts"},
	})
	if !contextHas(out, "Redis") {
		t.Fatalf("gold failed missed: %+v", out.Context)
	}
	if countType(out, "failed") > retrieve.PackTypeCap {
		t.Fatalf("type cap broken: %d %+v", countType(out, "failed"), out.Context)
	}
	if !contextHas(out, "jose") {
		t.Fatalf("decision crowded out: %+v", out.Context)
	}
}

func testFixtureBleed(t *testing.T) {
	st := dogfoodStore(t)
	dogfoodCatch(t, st, "acme/app", "grok-auth",
		grokLine("assistant", "Redis token bucket failed in src/middleware/auth.ts staging."))
	dogfoodCatch(t, st, "acme/app", "live-work",
		grokLine("assistant", "We decided to use jose, not jsonwebtoken, for Edge in src/middleware/auth.ts."))

	out := askAtNow(t, st, retrieve.Request{
		Project: "acme/app", Question: "rate limiting",
		Paths: []string{"src/middleware/auth.ts"},
	})
	// fixture session grok-auth must drop when live work exists
	for _, h := range out.Context {
		if h.Text == "Redis token bucket failed in src/middleware/auth.ts staging." && !contextHas(out, "jose") {
			t.Fatalf("fixture won without live decision: %+v", out.Context)
		}
	}
	if !contextHas(out, "jose") {
		t.Fatalf("live work missed: %+v", out.Context)
	}
}

func testCompactOscillate(t *testing.T) {
	st := dogfoodStore(t)
	p := testdataWrite(t, t.TempDir(), "osc.jsonl",
		grokLine("assistant", "We decided to use jose, not jsonwebtoken, for Edge in src/middleware/auth.ts."))
	if _, err := write.CatchUp(st, write.CatchUpRequest{JSONL: p, Project: "acme/app", SessionID: "osc"}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		// shrink (Grok compact)
		small := grokLine("user", "This session is being continued from a previous conversation.")
		if err := os.WriteFile(p, []byte(small), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := write.CatchUp(st, write.CatchUpRequest{JSONL: p, Project: "acme/app", SessionID: "osc", Source: "compact"}); err != nil {
			t.Fatal(err)
		}
		// grow again with a new durable sentence
		grow := small + grokLine("assistant", fmt.Sprintf("We decided to keep the limiter in-process in src/middleware/auth.ts pass %d instead of Redis.", i))
		if err := os.WriteFile(p, []byte(grow), 0o644); err != nil {
			t.Fatal(err)
		}
		res, err := write.CatchUp(st, write.CatchUpRequest{JSONL: p, Project: "acme/app", SessionID: "osc", Source: "turn"})
		if err != nil {
			t.Fatal(err)
		}
		if res.Noop && i == 0 {
			t.Fatalf("grow after shrink no-op: %+v", res)
		}
	}
	out := askAtNow(t, st, retrieve.Request{
		Project: "acme/app", Question: "limiter",
		Paths: []string{"src/middleware/auth.ts"},
	})
	if !contextHas(out, "limiter") && !contextHas(out, "jose") {
		t.Fatalf("nothing survived oscillate: %+v", out.Context)
	}
	if st.Cursor(p) > int64(1<<20) {
		t.Fatalf("cursor exploded: %d", st.Cursor(p))
	}
}

func testCrossProject(t *testing.T) {
	st := dogfoodStore(t)
	dogfoodCatch(t, st, "shop/billing", "b1",
		grokLine("assistant", "Stripe webhook failed in src/billing/stripe.ts last night."))
	dogfoodCatch(t, st, "acme/app", "a1",
		grokLine("assistant", "We decided to use jose, not jsonwebtoken, for Edge in src/middleware/auth.ts."))

	out := askAtNow(t, st, retrieve.Request{
		Project: "acme/app", Question: "what failed",
		Paths: []string{"src/middleware/auth.ts"},
	})
	if contextHas(out, "Stripe") {
		t.Fatalf("billing leaked into auth: %+v", out.Context)
	}
}

func testSecret(t *testing.T) {
	st := dogfoodStore(t)
	body := grokLine("assistant", "token ghp_"+strings.Repeat("x", 24)+" leaked in src/middleware/auth.ts.")
	body += grokLine("assistant", "We decided to use jose, not jsonwebtoken, for Edge in src/middleware/auth.ts.")
	p := testdataWrite(t, t.TempDir(), "sec.jsonl", body)
	res, err := write.CatchUp(st, write.CatchUpRequest{JSONL: p, Project: "acme/app", SessionID: "sec"})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(res.RawPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "ghp_") && !strings.Contains(string(raw), "_redacted") {
		t.Fatalf("secret survived raw: %s", raw)
	}
	out := askAtNow(t, st, retrieve.Request{Project: "acme/app", Question: "jose", Paths: []string{"src/middleware/auth.ts"}})
	if contextHas(out, "ghp_") {
		t.Fatalf("secret in context: %+v", out.Context)
	}
}

func testOffTopicFailed(t *testing.T) {
	st := dogfoodStore(t)
	var b strings.Builder
	b.WriteString(grokLine("assistant", "Who-reacted failed in preview in ios/LightboxView.swift."))
	b.WriteString(grokLine("assistant", "Networking package tests failed on an existing Sendable issue in src/net/Client.swift, not these changes."))
	b.WriteString(grokLine("assistant", "Android Photos-like lightbox open uses a same-window hero overlay above NavHost instead of a hard cut."))
	dogfoodCatch(t, st, "acme/app", "off", b.String())

	out := askAtNow(t, st, retrieve.Request{
		Project: "acme/app", Goal: "fix iOS lightbox swipe-down",
		Question: "what already failed or what did we decide about lightbox",
		Paths:    []string{"ios/LightboxView.swift"},
	})
	if !contextHas(out, "Who-reacted") && !contextHas(out, "lightbox") {
		t.Fatalf("on-topic missed: %+v", out.Context)
	}
	// Off-topic Sendable may still appear (ranking leftover) but must not
	// be the only failed and must not crowd out lightbox.
	if contextHas(out, "Sendable") && !contextHas(out, "Who-reacted") && !contextHas(out, "lightbox") {
		t.Fatalf("off-topic failed won: %+v", out.Context)
	}
}

func testWarningsBound(t *testing.T) {
	st := dogfoodStore(t)
	dogfoodCatch(t, st, "acme/app", "warn",
		grokLine("assistant", "Redis token bucket failed in src/middleware/auth.ts staging.")+
			grokLine("assistant", "We decided to use jose, not jsonwebtoken, for Edge in src/middleware/auth.ts."))
	out := askAtNow(t, st, retrieve.Request{
		Project: "acme/app", Goal: "add rate limiting",
		Paths: []string{"src/middleware/auth.ts"},
	})
	ids := map[string]bool{}
	for _, h := range out.Context {
		ids[h.ID] = true
	}
	for _, w := range out.Warnings {
		cited := false
		for id := range ids {
			if strings.Contains(w, id) {
				cited = true
				break
			}
		}
		if !cited {
			t.Fatalf("warning cites unpacked id: %s context=%+v", w, out.Context)
		}
	}
}

func testConcurrentPoison(t *testing.T) {
	st := dogfoodStore(t)
	dir := t.TempDir()
	var wg sync.WaitGroup
	errc := make(chan error, 16)
	for i := 0; i < 8; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			var b strings.Builder
			b.WriteString(grokLine("assistant", fmt.Sprintf("Helper %d failed in src/mod/file%d.ts during compile.", i, i)))
			b.WriteString(grokLine("assistant", fmt.Sprintf("Checking #%d failure and re-pushing both.", 4000+i)))
			b.WriteString(grokLine("assistant", "## Investigation: why those uploads failed"))
			p := filepath.Join(dir, fmt.Sprintf("c%d.jsonl", i))
			if err := os.WriteFile(p, []byte(b.String()), 0o644); err != nil {
				errc <- err
				return
			}
			_, err := write.CatchUp(st, write.CatchUpRequest{
				JSONL: p, Project: "acme/app", Harness: "grok",
				SessionID: fmt.Sprintf("c%d", i),
			})
			if err != nil {
				errc <- err
			}
		}()
	}
	wg.Wait()
	close(errc)
	for err := range errc {
		t.Fatal(err)
	}
	var askErr error
	var askWg sync.WaitGroup
	for i := 0; i < 8; i++ {
		askWg.Add(1)
		go func() {
			defer askWg.Done()
			var err error
			for try := 0; try < 8; try++ {
				_, err = retrieve.Ask(st, retrieve.Request{Project: "acme/app", Question: "file3 compile", Paths: []string{"src/mod/file3.ts"}})
				if err == nil || !strings.Contains(err.Error(), "BUSY") {
					break
				}
				time.Sleep(time.Duration(try+1) * 20 * time.Millisecond)
			}
			if err != nil && askErr == nil {
				askErr = err
			}
		}()
	}
	askWg.Wait()
	if askErr != nil {
		t.Fatal(askErr)
	}
}

func testBacktickFailed(t *testing.T) {
	st := dogfoodStore(t)
	dogfoodCatch(t, st, "acme/app", "ticks",
		grokLine("assistant", "The live ask just returned five `failed`s, and four look like extract noise.")+
			grokLine("assistant", "Who-reacted failed in preview in ios/LightboxView.swift."))
	out := askAtNow(t, st, retrieve.Request{
		Project: "acme/app", Question: "what failed",
		Paths: []string{"ios/LightboxView.swift"},
	})
	if contextHas(out, "five") {
		t.Fatalf("backtick-only failed packed: %+v", out.Context)
	}
}

func testTruncated(t *testing.T) {
	st := dogfoodStore(t)
	dogfoodCatch(t, st, "acme/app", "trunc",
		grokLine("assistant", "Call scheduler_create with its task_id and only the changed fields; do not")+
			grokLine("assistant", "This device pass exercised the **retryable failure** path (`.")+
			grokLine("assistant", "We decided to use jose, not jsonwebtoken, for Edge in src/middleware/auth.ts."))
	out := askAtNow(t, st, retrieve.Request{
		Project: "acme/app", Question: "jose",
		Paths: []string{"src/middleware/auth.ts"},
	})
	if contextHasAny(out, "do not", "path (`.") {
		t.Fatalf("truncated packed: %+v", out.Context)
	}
}

func testFailedAsObject(t *testing.T) {
	st := dogfoodStore(t)
	dogfoodCatch(t, st, "acme/app", "obj",
		grokLine("assistant", "So the Retry button is live: it re-queues failed items and they land on the server/grid.")+
			grokLine("assistant", "**Upload Complete** sheet: **7 of 10 uploaded, 3 failed**, each with **Could not load this photo**, and a Retry that does nothing."))
	out := askAtNow(t, st, retrieve.Request{
		Project: "acme/app", Question: "what failed about upload retry",
	})
	if contextHas(out, "re-queues failed items") {
		t.Fatalf("success-about-failed-items packed: %+v", out.Context)
	}
}

func testAskPacketEcho(t *testing.T) {
	st := dogfoodStore(t)
	packet := `{"context":[{"id":"x","type":"failed","text":"Redis token bucket failed in src/middleware/auth.ts staging."}],"warnings":["A prior attempt failed"],"tokens":12,"project":"acme/app"}`
	var b strings.Builder
	b.WriteString(grokLine("assistant", packet))
	b.WriteString(grokLine("assistant", "We decided to use jose, not jsonwebtoken, for Edge in src/middleware/auth.ts."))
	dogfoodCatch(t, st, "acme/app", "echo", b.String())
	out := askAtNow(t, st, retrieve.Request{
		Project: "acme/app", Question: "jose", Paths: []string{"src/middleware/auth.ts"},
	})
	if contextHas(out, "Redis token bucket") {
		t.Fatalf("ask packet echoed: %+v", out.Context)
	}
	if !contextHas(out, "jose") {
		t.Fatalf("jose missed: %+v", out.Context)
	}
}

func testConstraintFlood(t *testing.T) {
	st := dogfoodStore(t)
	var b strings.Builder
	b.WriteString(grokLine("user", "Always never log Authorization headers in src/middleware/auth.ts."))
	for i := 0; i < 20; i++ {
		b.WriteString(grokLine("user", fmt.Sprintf("Always never bikeshed the font in the settings panel number %d.", i)))
	}
	dogfoodCatch(t, st, "acme/app", "cflood", b.String())
	out := askAtNow(t, st, retrieve.Request{
		Project: "acme/app", Question: "Authorization headers",
		Paths: []string{"src/middleware/auth.ts"},
	})
	if !contextHas(out, "Authorization") {
		t.Fatalf("real constraint missed: %+v", out.Context)
	}
	if contextHas(out, "bikeshed") {
		t.Fatalf("pathless constraint flood packed: %+v", out.Context)
	}
}

func testStateFlood(t *testing.T) {
	st := dogfoodStore(t)
	var b strings.Builder
	b.WriteString(grokLine("assistant", "We decided to use jose, not jsonwebtoken, for Edge in src/middleware/auth.ts."))
	for i := 0; i < 30; i++ {
		b.WriteString(grokLine("assistant", fmt.Sprintf("Working on src/ui/Button%d.tsx hover pass now implementing next.", i)))
	}
	dogfoodCatch(t, st, "acme/app", "sflood", b.String())
	out := askAtNow(t, st, retrieve.Request{
		Project: "acme/app", Question: "why not jsonwebtoken",
		Paths: []string{"src/middleware/auth.ts"},
	})
	if !contextHas(out, "jose") {
		t.Fatalf("decision starved by states: %+v", out.Context)
	}
}

func testSameSessionConcurrent(t *testing.T) {
	st := dogfoodStore(t)
	p := testdataWrite(t, t.TempDir(), "same.jsonl",
		grokLine("assistant", "We decided to use jose, not jsonwebtoken, for Edge in src/middleware/auth.ts."))
	var wg sync.WaitGroup
	errc := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := write.CatchUp(st, write.CatchUpRequest{
				JSONL: p, Project: "acme/app", Harness: "grok", SessionID: "same",
			})
			if err != nil {
				errc <- err
			}
		}()
	}
	wg.Wait()
	close(errc)
	for err := range errc {
		t.Fatal(err)
	}
	n := 0
	claims, _ := st.ListActive("acme/app")
	for _, c := range claims {
		if strings.Contains(c.Text, "jose") {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("concurrent same-session wrote %d jose claims", n)
	}
}

func testThrewAbortStatus(t *testing.T) {
	st := dogfoodStore(t)
	var b strings.Builder
	b.WriteString(grokLine("assistant", "The CI job threw after the hook timeout so we abort the deploy."))
	b.WriteString(grokLine("assistant", "The limiter threw in src/middleware/auth.ts and we had to abort the Redis pool."))
	dogfoodCatch(t, st, "acme/app", "threw", b.String())
	out := askAtNow(t, st, retrieve.Request{
		Project: "acme/app", Goal: "add rate limiting",
		Paths: []string{"src/middleware/auth.ts"},
	})
	if contextHas(out, "CI job threw") {
		t.Fatalf("status threw packed: %+v", out.Context)
	}
	if !contextHas(out, "limiter threw") {
		t.Fatalf("real threw missed: %+v", out.Context)
	}
}

func testFailureInFilename(t *testing.T) {
	st := dogfoodStore(t)
	dogfoodCatch(t, st, "acme/app", "fn",
		grokLine("assistant", "We decided to keep src/failure/handler.ts instead of Redis."))
	out := askAtNow(t, st, retrieve.Request{
		Project: "acme/app", Question: "handler",
		Paths: []string{"src/failure/handler.ts"},
	})
	for _, h := range out.Context {
		if h.Type == "failed" && strings.Contains(h.Text, "handler.ts") {
			t.Fatalf("filename failure classified failed: %+v", h)
		}
	}
	if !contextHas(out, "handler.ts") {
		t.Fatalf("decision missed: %+v", out.Context)
	}
}

func testWindowsUNC(t *testing.T) {
	got := redact.FilterPaths([]string{
		`C:\Users\jay\secret.md`,
		`//nas/share/id_rsa`,
		`src/ok.go`,
	})
	if len(got) != 1 || got[0] != "src/ok.go" {
		t.Fatalf("%v", got)
	}
}

func testYearOldGold(t *testing.T) {
	st := dogfoodStore(t)
	if _, err := st.WriteClaim(claim.Record{
		ID: "OLDJOSE", Type: "decision", ProjectKey: "acme/app",
		Text:  "We decided to use jose, not jsonwebtoken, for Edge in src/middleware/auth.ts.",
		Paths: []string{"src/middleware/auth.ts"}, CreatedAt: "2025-08-17T00:00:00Z",
		Harness: "grok", SessionID: "old", Source: "import", Status: "active",
	}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 20; i++ {
		if _, err := st.WriteClaim(claim.Record{
			Type: "state", ProjectKey: "acme/app",
			Text:  fmt.Sprintf("Working on src/ui/n%d.ts hover pass now implementing next.", i),
			Paths: []string{fmt.Sprintf("src/ui/n%d.ts", i)}, CreatedAt: "2026-08-01T00:00:00Z",
			Harness: "grok", SessionID: "new", Source: "import", Status: "active",
		}); err != nil {
			t.Fatal(err)
		}
	}
	out := askAtNow(t, st, retrieve.Request{
		Project: "acme/app", Question: "why not jsonwebtoken",
		Paths: []string{"src/middleware/auth.ts"},
	})
	if !contextHas(out, "jose") {
		t.Fatalf("year-old gold dropped: %+v", out.Context)
	}
}

func testPartialLine(t *testing.T) {
	st := dogfoodStore(t)
	dir := t.TempDir()
	p := testdataWrite(t, dir, "part.jsonl", `{"type":"assistant","content":"We decided to use jose`)
	res, err := write.CatchUp(st, write.CatchUpRequest{JSONL: p, Project: "acme/app", SessionID: "part"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Copied != 0 && res.Extracted != 0 {
		t.Fatalf("incomplete line extracted: %+v", res)
	}
	full := grokLine("assistant", "We decided to use jose, not jsonwebtoken, for Edge in src/middleware/auth.ts.")
	if err := os.WriteFile(p, []byte(full), 0o644); err != nil {
		t.Fatal(err)
	}
	// shrink relative to a "cursor past incomplete" — file may be smaller or rewritten
	res, err = write.CatchUp(st, write.CatchUpRequest{JSONL: p, Project: "acme/app", SessionID: "part"})
	if err != nil {
		t.Fatal(err)
	}
	out := askAtNow(t, st, retrieve.Request{Project: "acme/app", Question: "jose", Paths: []string{"src/middleware/auth.ts"}})
	if !contextHas(out, "jose") {
		t.Fatalf("completed line missed: copied=%d extracted=%d ctx=%+v", res.Copied, res.Extracted, out.Context)
	}
}

func testSessionIDEscape(t *testing.T) {
	st := dogfoodStore(t)
	p := testdataWrite(t, t.TempDir(), "safe.jsonl",
		grokLine("assistant", "We decided to use jose, not jsonwebtoken, for Edge in src/middleware/auth.ts."))
	res, err := write.CatchUp(st, write.CatchUpRequest{
		JSONL: p, Project: "acme/app", Harness: "grok", SessionID: "../../etc/passwd",
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(filepath.Clean(res.RawPath), "/etc/") {
		t.Fatalf("session id escaped store: %s", res.RawPath)
	}
	rel, err := filepath.Rel(st.Root, res.RawPath)
	if err != nil || strings.HasPrefix(rel, "..") {
		t.Fatalf("raw outside store: %s (%v)", res.RawPath, err)
	}
}

func testAssistantQuoteConstraint(t *testing.T) {
	st := dogfoodStore(t)
	dogfoodCatch(t, st, "acme/app", "q",
		grokLine("assistant", `The user said: "Always never log Authorization headers in src/middleware/auth.ts."`)+
			grokLine("user", "Always never log Authorization headers in src/middleware/auth.ts."))
	out := askAtNow(t, st, retrieve.Request{
		Project: "acme/app", Question: "Authorization headers",
		Paths: []string{"src/middleware/auth.ts"},
	})
	if !contextHas(out, "Authorization") {
		t.Fatalf("user constraint missed: %+v", out.Context)
	}
}

func testAskTraversalPaths(t *testing.T) {
	st := dogfoodStore(t)
	dogfoodCatch(t, st, "acme/app", "trav",
		grokLine("assistant", "We decided to use jose, not jsonwebtoken, for Edge in src/middleware/auth.ts."))
	out := askAtNow(t, st, retrieve.Request{
		Project: "acme/app", Question: "jose",
		Paths:         []string{"../.ssh/id_rsa", "/etc/passwd", "src/middleware/auth.ts"},
		WorkspaceRoot: t.TempDir(),
	})
	if !contextHas(out, "jose") {
		t.Fatalf("ask with dirty paths missed gold: %+v", out.Context)
	}
}

func testClaudeParts(t *testing.T) {
	st := dogfoodStore(t)
	line := `{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"We decided to use jose, not jsonwebtoken, for Edge in src/middleware/auth.ts."}]}}` + "\n"
	p := testdataWrite(t, t.TempDir(), "claude.jsonl", line)
	if _, err := write.CatchUp(st, write.CatchUpRequest{
		JSONL: p, Project: "acme/app", Harness: "claude", SessionID: "cl",
	}); err != nil {
		t.Fatal(err)
	}
	out := askAtNow(t, st, retrieve.Request{Project: "acme/app", Question: "jose", Paths: []string{"src/middleware/auth.ts"}})
	if !contextHas(out, "jose") {
		t.Fatalf("claude parts missed: %+v", out.Context)
	}
}

func testPrefixedMCPTool(t *testing.T) {
	st := dogfoodStore(t)
	// Claude-style: tool_result is nested in a user content array, so
	// extract sees it unless isOwnTool recognizes lossless__* names.
	body := strings.Join([]string{
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","id":"g1","name":"lossless__get_record","input":{"id":"01JFAIL"}}]}}`,
		`{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"g1","content":"{\"id\":\"01JFAIL\",\"type\":\"failed\",\"text\":\"Redis token bucket failed in src/middleware/auth.ts staging.\"}"}]}}`,
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","id":"a1","name":"mcp__lossless__ask","input":{}}]}}`,
		`{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"a1","content":"{\"context\":[{\"id\":\"01JFAIL\",\"type\":\"failed\",\"text\":\"Redis token bucket failed in src/middleware/auth.ts staging.\"}],\"warnings\":[\"A prior attempt failed\"],\"tokens\":8,\"project\":\"acme/app\"}"}]}}`,
		`{"type":"assistant","content":"We decided to use jose, not jsonwebtoken, for Edge in src/middleware/auth.ts."}`,
	}, "\n") + "\n"
	p := testdataWrite(t, t.TempDir(), "mcp.jsonl", body)
	if _, err := write.CatchUp(st, write.CatchUpRequest{
		JSONL: p, Project: "acme/app", Harness: "grok", SessionID: "mcp",
	}); err != nil {
		t.Fatal(err)
	}
	out := askAtNow(t, st, retrieve.Request{
		Project: "acme/app", Question: "jose", Paths: []string{"src/middleware/auth.ts"},
	})
	if contextHas(out, "Redis token bucket") {
		t.Fatalf("prefixed MCP tool echoed: %+v", out.Context)
	}
	if !contextHas(out, "jose") {
		t.Fatalf("jose missed: %+v", out.Context)
	}
}

func testFencedAskPacket(t *testing.T) {
	st := dogfoodStore(t)
	packet := "```json\n" +
		`{"context":[{"id":"x","type":"failed","text":"Redis token bucket failed in src/middleware/auth.ts staging."}],"warnings":["A prior attempt failed"],"tokens":12,"project":"acme/app"}` +
		"\n```\nWe decided to use jose, not jsonwebtoken, for Edge in src/middleware/auth.ts."
	dogfoodCatch(t, st, "acme/app", "fence", grokLine("assistant", packet))
	out := askAtNow(t, st, retrieve.Request{
		Project: "acme/app", Question: "jose", Paths: []string{"src/middleware/auth.ts"},
	})
	if contextHas(out, "Redis token bucket") {
		t.Fatalf("fenced ask packet echoed: %+v", out.Context)
	}
	if !contextHas(out, "jose") {
		t.Fatalf("jose missed: %+v", out.Context)
	}
}

func testPrefixedAskJSON(t *testing.T) {
	st := dogfoodStore(t)
	text := "lossless ask returned:\n" +
		`{"context":[{"id":"x","type":"failed","text":"Redis token bucket failed in src/middleware/auth.ts staging."}],"warnings":["A prior attempt failed"],"tokens":12,"project":"acme/app"}`
	var b strings.Builder
	b.WriteString(grokLine("assistant", text))
	b.WriteString(grokLine("assistant", "We decided to use jose, not jsonwebtoken, for Edge in src/middleware/auth.ts."))
	dogfoodCatch(t, st, "acme/app", "prefjson", b.String())
	out := askAtNow(t, st, retrieve.Request{
		Project: "acme/app", Question: "jose", Paths: []string{"src/middleware/auth.ts"},
	})
	if contextHas(out, "Redis token bucket") {
		t.Fatalf("prefixed ask JSON echoed: %+v", out.Context)
	}
	if !contextHas(out, "jose") {
		t.Fatalf("jose missed: %+v", out.Context)
	}
}

func testDecidedToRevert(t *testing.T) {
	st := dogfoodStore(t)
	dogfoodCatch(t, st, "acme/app", "revert",
		grokLine("assistant", "We decided to revert the Redis limiter and keep jose in src/middleware/auth.ts."))
	out := askAtNow(t, st, retrieve.Request{
		Project: "acme/app", Question: "why not Redis",
		Paths: []string{"src/middleware/auth.ts"},
	})
	if !contextHas(out, "jose") && !contextHas(out, "revert") {
		t.Fatalf("revert decision missed: %+v", out.Context)
	}
	for _, h := range out.Context {
		if h.Type == "failed" && strings.Contains(h.Text, "revert") {
			t.Fatalf("decided-to-revert classified failed: %+v", h)
		}
	}
}

func testDotGitPath(t *testing.T) {
	st := dogfoodStore(t)
	var b strings.Builder
	b.WriteString(grokLine("assistant", "The pre-commit hook failed in .git/hooks/pre-commit.sh so rebase stopped."))
	b.WriteString(grokLine("assistant", "Who-reacted failed in preview in ios/LightboxView.swift."))
	dogfoodCatch(t, st, "acme/app", "dotgit", b.String())
	claims, _ := st.ListActive("acme/app")
	for _, c := range claims {
		for _, p := range c.Paths {
			if strings.Contains(p, ".git/") || strings.HasPrefix(p, ".git") {
				t.Fatalf(".git path stored: %v in %s", c.Paths, c.Text)
			}
		}
	}
	out := askAtNow(t, st, retrieve.Request{
		Project: "acme/app", Question: "what failed",
		Paths: []string{"ios/LightboxView.swift"},
	})
	if contextHas(out, "pre-commit") {
		t.Fatalf(".git-grounded failed packed: %+v", out.Context)
	}
	if !contextHas(out, "Who-reacted") {
		t.Fatalf("product failed missed: %+v", out.Context)
	}
}

func testErrorFlagDecision(t *testing.T) {
	st := dogfoodStore(t)
	body := `{"type":"assistant","error":true,"content":"We decided to use jose, not jsonwebtoken, for Edge in src/middleware/auth.ts."}` + "\n" +
		`{"type":"assistant","message":{"role":"assistant","isError":true,"content":[{"type":"text","text":"We decided to keep the limiter in-process in src/middleware/auth.ts."}]}}` + "\n"
	p := testdataWrite(t, t.TempDir(), "errflag.jsonl", body)
	if _, err := write.CatchUp(st, write.CatchUpRequest{
		JSONL: p, Project: "acme/app", Harness: "claude", SessionID: "errflag",
	}); err != nil {
		t.Fatal(err)
	}
	out := askAtNow(t, st, retrieve.Request{
		Project: "acme/app", Question: "jose",
		Paths: []string{"src/middleware/auth.ts"},
	})
	if !contextHas(out, "jose") && !contextHas(out, "limiter") {
		t.Fatalf("isError stomped decision: %+v", out.Context)
	}
	for _, h := range out.Context {
		if h.Type == "failed" && (strings.Contains(h.Text, "jose") || strings.Contains(h.Text, "limiter")) {
			t.Fatalf("decision reclassified failed via error flag: %+v", h)
		}
	}
}

func testNumberedList(t *testing.T) {
	st := dogfoodStore(t)
	var b strings.Builder
	b.WriteString(grokLine("assistant", "We decided to use jose, not jsonwebtoken, for Edge in src/middleware/auth.ts."))
	b.WriteString(grokLine("assistant", "1. Redis limiter **failed** (twice) + warning: do not repeat"))
	b.WriteString(grokLine("assistant", "2. The live pack returned five `failed`s from extract noise"))
	dogfoodCatch(t, st, "acme/app", "num", b.String())
	claims, _ := st.ListActive("acme/app")
	for _, c := range claims {
		if strings.Contains(c.Text, "Redis limiter") || strings.Contains(c.Text, "five `failed`") {
			t.Fatalf("numbered list extracted: %+v", c)
		}
	}
	out := askAtNow(t, st, retrieve.Request{
		Project: "acme/app", Question: "jose",
		Paths: []string{"src/middleware/auth.ts"},
	})
	if !contextHas(out, "jose") {
		t.Fatalf("jose missed: %+v", out.Context)
	}
}

func testNeverMind(t *testing.T) {
	st := dogfoodStore(t)
	var b strings.Builder
	b.WriteString(grokLine("user", "Always never log Authorization headers in src/middleware/auth.ts."))
	b.WriteString(grokLine("user", "Never mind the lint on src/middleware/auth.ts, keep going."))
	dogfoodCatch(t, st, "acme/app", "nvm", b.String())
	out := askAtNow(t, st, retrieve.Request{
		Project: "acme/app", Question: "Authorization headers",
		Paths: []string{"src/middleware/auth.ts"},
	})
	if !contextHas(out, "Authorization") {
		t.Fatalf("real constraint missed: %+v", out.Context)
	}
	if contextHas(out, "Never mind") || contextHas(out, "lint") {
		t.Fatalf("never-mind packed: %+v", out.Context)
	}
}

func testHugeMessageTail(t *testing.T) {
	st := dogfoodStore(t)
	// parse currently Skip:true at >8000; gold lives in the last 200 chars.
	dump := strings.Repeat("stack frame in vendor/lib/noise.go\n", 400)
	text := dump + "We decided to use jose, not jsonwebtoken, for Edge in src/middleware/auth.ts."
	if len(text) <= 8000 {
		t.Fatalf("fixture too short: %d", len(text))
	}
	dogfoodCatch(t, st, "acme/app", "huge", grokLine("assistant", text))
	out := askAtNow(t, st, retrieve.Request{
		Project: "acme/app", Question: "jose",
		Paths: []string{"src/middleware/auth.ts"},
	})
	if !contextHas(out, "jose") {
		t.Fatalf("huge-message tail gold missed: %+v", out.Context)
	}
}

func testPlanningGoWith(t *testing.T) {
	st := dogfoodStore(t)
	var b strings.Builder
	b.WriteString(grokLine("assistant", "I'll go with postgres instead of mysql in src/db/client.ts."))
	b.WriteString(grokLine("assistant", "We decided to use jose, not jsonwebtoken, for Edge in src/middleware/auth.ts."))
	dogfoodCatch(t, st, "acme/app", "gowith", b.String())
	claims, _ := st.ListActive("acme/app")
	for _, c := range claims {
		if strings.Contains(c.Text, "postgres") || strings.Contains(c.Text, "I'll go with") {
			t.Fatalf("planning go-with extracted: %+v", c)
		}
	}
	out := askAtNow(t, st, retrieve.Request{
		Project: "acme/app", Question: "why not jsonwebtoken",
		Paths: []string{"src/middleware/auth.ts"},
	})
	if !contextHas(out, "jose") {
		t.Fatalf("jose missed: %+v", out.Context)
	}
}

func testDontPushYet(t *testing.T) {
	st := dogfoodStore(t)
	var b strings.Builder
	b.WriteString(grokLine("user", "Always never log Authorization headers in src/middleware/auth.ts."))
	b.WriteString(grokLine("user", "Don't push yet I'll review src/middleware/auth.ts after lunch."))
	b.WriteString(grokLine("user", "Don't merge yet until the limiter is in."))
	dogfoodCatch(t, st, "acme/app", "pushyet", b.String())
	out := askAtNow(t, st, retrieve.Request{
		Project: "acme/app", Question: "Authorization headers",
		Paths: []string{"src/middleware/auth.ts"},
	})
	if !contextHas(out, "Authorization") {
		t.Fatalf("real constraint missed: %+v", out.Context)
	}
	if contextHasAny(out, "Don't push", "Don't merge") {
		t.Fatalf("session-op packed: %+v", out.Context)
	}
}

func testExceptionTo(t *testing.T) {
	st := dogfoodStore(t)
	var b strings.Builder
	b.WriteString(grokLine("assistant", "That's an exception to the rule we always use jose in src/middleware/auth.ts."))
	b.WriteString(grokLine("assistant", "We decided to use jose, not jsonwebtoken, for Edge in src/middleware/auth.ts."))
	dogfoodCatch(t, st, "acme/app", "exto", b.String())
	out := askAtNow(t, st, retrieve.Request{
		Project: "acme/app", Question: "jose",
		Paths: []string{"src/middleware/auth.ts"},
	})
	if contextHas(out, "exception to") {
		t.Fatalf("exception-to-the-rule packed as failed: %+v", out.Context)
	}
	if !contextHas(out, "jose") {
		t.Fatalf("jose missed: %+v", out.Context)
	}
}

func testGitHubActionsStatus(t *testing.T) {
	st := dogfoodStore(t)
	var b strings.Builder
	b.WriteString(grokLine("assistant", "The GitHub Actions workflow failed so the deploy did not start."))
	b.WriteString(grokLine("assistant", "Who-reacted failed in preview in ios/LightboxView.swift."))
	dogfoodCatch(t, st, "acme/app", "gha", b.String())
	out := askAtNow(t, st, retrieve.Request{
		Project: "acme/app", Question: "what failed",
		Paths: []string{"ios/LightboxView.swift"},
	})
	if contextHas(out, "GitHub Actions") {
		t.Fatalf("CI workflow status packed: %+v", out.Context)
	}
	if !contextHas(out, "Who-reacted") {
		t.Fatalf("product failed missed: %+v", out.Context)
	}
}

func testSystemReminderMid(t *testing.T) {
	st := dogfoodStore(t)
	text := "Always never log Authorization headers in src/middleware/auth.ts.\n" +
		"<system-reminder>\nAlways never use the simulator. Don't change source. Don't delete data.\n</system-reminder>"
	dogfoodCatch(t, st, "acme/app", "sysrem", grokLine("user", text))
	out := askAtNow(t, st, retrieve.Request{
		Project: "acme/app", Question: "Authorization headers",
		Paths: []string{"src/middleware/auth.ts"},
	})
	if !contextHas(out, "Authorization") {
		t.Fatalf("real constraint missed: %+v", out.Context)
	}
	if contextHasAny(out, "simulator", "Don't change source", "Don't delete") {
		t.Fatalf("system-reminder packed: %+v", out.Context)
	}
}

func testAssistantQuoteDecision(t *testing.T) {
	st := dogfoodStore(t)
	var b strings.Builder
	b.WriteString(grokLine("assistant", `The previous session said: "We decided to use mongo, not postgres, in src/db/client.ts."`))
	b.WriteString(grokLine("assistant", "We decided to use jose, not jsonwebtoken, for Edge in src/middleware/auth.ts."))
	dogfoodCatch(t, st, "acme/app", "qdec", b.String())
	claims, _ := st.ListActive("acme/app")
	for _, c := range claims {
		if strings.Contains(c.Text, "mongo") {
			t.Fatalf("quoted old decision extracted: %+v", c)
		}
	}
	out := askAtNow(t, st, retrieve.Request{
		Project: "acme/app", Question: "jose", Paths: []string{"src/middleware/auth.ts"},
	})
	if !contextHas(out, "jose") {
		t.Fatalf("jose missed: %+v", out.Context)
	}
}

func testNodeModulesPath(t *testing.T) {
	st := dogfoodStore(t)
	var b strings.Builder
	b.WriteString(grokLine("assistant", "The lodash suite failed in node_modules/lodash/index.js during install."))
	b.WriteString(grokLine("assistant", "Who-reacted failed in preview in ios/LightboxView.swift."))
	dogfoodCatch(t, st, "acme/app", "nm", b.String())
	claims, _ := st.ListActive("acme/app")
	for _, c := range claims {
		for _, p := range c.Paths {
			if strings.Contains(p, "node_modules") {
				t.Fatalf("node_modules path stored: %v in %s", c.Paths, c.Text)
			}
		}
		if strings.Contains(c.Text, "lodash") {
			t.Fatalf("node_modules failed extracted: %+v", c)
		}
	}
	out := askAtNow(t, st, retrieve.Request{
		Project: "acme/app", Question: "what failed",
		Paths: []string{"ios/LightboxView.swift"},
	})
	if contextHas(out, "lodash") {
		t.Fatalf("node_modules failed packed: %+v", out.Context)
	}
	if !contextHas(out, "Who-reacted") {
		t.Fatalf("product failed missed: %+v", out.Context)
	}
}

func testRememberProse(t *testing.T) {
	st := dogfoodStore(t)
	var b strings.Builder
	b.WriteString(grokLine("assistant", "Remembered: Redis token bucket failed in src/middleware/auth.ts staging."))
	b.WriteString(grokLine("assistant", "We decided to use jose, not jsonwebtoken, for Edge in src/middleware/auth.ts."))
	dogfoodCatch(t, st, "acme/app", "remp", b.String())
	claims, _ := st.ListActive("acme/app")
	for _, c := range claims {
		if strings.HasPrefix(c.Text, "Remembered:") || strings.Contains(c.Text, "Redis token bucket") {
			t.Fatalf("remember prose extracted: %+v", c)
		}
	}
	out := askAtNow(t, st, retrieve.Request{
		Project: "acme/app", Question: "jose", Paths: []string{"src/middleware/auth.ts"},
	})
	if !contextHas(out, "jose") {
		t.Fatalf("jose missed: %+v", out.Context)
	}
}

func testHyphenatedMCPTool(t *testing.T) {
	st := dogfoodStore(t)
	body := strings.Join([]string{
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","id":"g1","name":"lossless-get-record","input":{"id":"01JFAIL"}}]}}`,
		`{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"g1","content":"{\"id\":\"01JFAIL\",\"type\":\"failed\",\"text\":\"Redis token bucket failed in src/middleware/auth.ts staging.\"}"}]}}`,
		`{"type":"assistant","content":"We decided to use jose, not jsonwebtoken, for Edge in src/middleware/auth.ts."}`,
	}, "\n") + "\n"
	p := testdataWrite(t, t.TempDir(), "hyphen.jsonl", body)
	if _, err := write.CatchUp(st, write.CatchUpRequest{
		JSONL: p, Project: "acme/app", Harness: "claude", SessionID: "hyphen",
	}); err != nil {
		t.Fatal(err)
	}
	out := askAtNow(t, st, retrieve.Request{
		Project: "acme/app", Question: "jose", Paths: []string{"src/middleware/auth.ts"},
	})
	if contextHas(out, "Redis token bucket") {
		t.Fatalf("hyphenated MCP tool echoed: %+v", out.Context)
	}
	if !contextHas(out, "jose") {
		t.Fatalf("jose missed: %+v", out.Context)
	}
}

func testDontWait(t *testing.T) {
	st := dogfoodStore(t)
	var b strings.Builder
	b.WriteString(grokLine("user", "Always never log Authorization headers in src/middleware/auth.ts."))
	b.WriteString(grokLine("user", "Don't wait for me, just keep going on src/middleware/auth.ts."))
	dogfoodCatch(t, st, "acme/app", "dwait", b.String())
	claims, _ := st.ListActive("acme/app")
	for _, c := range claims {
		if strings.Contains(c.Text, "Don't wait") || strings.Contains(strings.ToLower(c.Text), "just keep going") {
			t.Fatalf("session-op extracted: %+v", c)
		}
	}
	out := askAtNow(t, st, retrieve.Request{
		Project: "acme/app", Question: "Authorization headers",
		Paths: []string{"src/middleware/auth.ts"},
	})
	if !contextHas(out, "Authorization") {
		t.Fatalf("real constraint missed: %+v", out.Context)
	}
}

func testPiTypeMessage(t *testing.T) {
	st := dogfoodStore(t)
	body := strings.Join([]string{
		`{"type":"session","version":3,"id":"u1","cwd":"/ws"}`,
		`{"type":"message","id":"a1","message":{"role":"user","content":"Always never log Authorization headers in src/middleware/auth.ts."}}`,
		`{"type":"message","id":"a2","message":{"role":"assistant","content":[{"type":"thinking","thinking":"hmm"},{"type":"text","text":"We decided to use jose, not jsonwebtoken, for Edge in src/middleware/auth.ts."}]}}`,
	}, "\n") + "\n"
	p := testdataWrite(t, t.TempDir(), "pi.jsonl", body)
	if _, err := write.CatchUp(st, write.CatchUpRequest{
		JSONL: p, Project: "acme/app", Harness: "pi", SessionID: "pi1",
	}); err != nil {
		t.Fatal(err)
	}
	out := askAtNow(t, st, retrieve.Request{
		Project: "acme/app", Question: "jose", Paths: []string{"src/middleware/auth.ts"},
	})
	if !contextHas(out, "jose") {
		t.Fatalf("pi type=message missed: %+v", out.Context)
	}
	if !contextHas(out, "Authorization") {
		t.Fatalf("pi user constraint missed: %+v", out.Context)
	}
}

func testPastedGoTest(t *testing.T) {
	st := dogfoodStore(t)
	var b strings.Builder
	b.WriteString(grokLine("assistant", "--- FAIL: TestLightbox (0.12s)\n    lightbox_test.go:44: assertion failed"))
	b.WriteString(grokLine("assistant", "ok  \tlossless/eval\t1.258s"))
	b.WriteString(grokLine("assistant", "Who-reacted failed in preview in ios/LightboxView.swift."))
	dogfoodCatch(t, st, "acme/app", "gotest", b.String())
	out := askAtNow(t, st, retrieve.Request{
		Project: "acme/app", Question: "what failed",
		Paths: []string{"ios/LightboxView.swift"},
	})
	if contextHasAny(out, "FAIL: TestLightbox", "assertion failed", "ok  \tlossless") {
		t.Fatalf("pasted go test packed: %+v", out.Context)
	}
	if !contextHas(out, "Who-reacted") {
		t.Fatalf("product failed missed: %+v", out.Context)
	}
}

func testIllImplement(t *testing.T) {
	st := dogfoodStore(t)
	var b strings.Builder
	b.WriteString(grokLine("assistant", "I'll implement postgres instead of mysql in src/db/client.ts."))
	b.WriteString(grokLine("assistant", "We decided to use jose, not jsonwebtoken, for Edge in src/middleware/auth.ts."))
	dogfoodCatch(t, st, "acme/app", "impl", b.String())
	claims, _ := st.ListActive("acme/app")
	for _, c := range claims {
		if strings.Contains(c.Text, "postgres") || strings.Contains(c.Text, "I'll implement") {
			t.Fatalf("planning implement extracted: %+v", c)
		}
	}
	out := askAtNow(t, st, retrieve.Request{
		Project: "acme/app", Question: "jose", Paths: []string{"src/middleware/auth.ts"},
	})
	if !contextHas(out, "jose") {
		t.Fatalf("jose missed: %+v", out.Context)
	}
}

func testDistPath(t *testing.T) {
	st := dogfoodStore(t)
	var b strings.Builder
	b.WriteString(grokLine("assistant", "The bundle step failed in dist/assets/index.js after minify."))
	b.WriteString(grokLine("assistant", "Who-reacted failed in preview in ios/LightboxView.swift."))
	dogfoodCatch(t, st, "acme/app", "dist", b.String())
	claims, _ := st.ListActive("acme/app")
	for _, c := range claims {
		for _, p := range c.Paths {
			if strings.HasPrefix(p, "dist/") || strings.Contains(p, "/dist/") {
				t.Fatalf("dist path stored: %v in %s", c.Paths, c.Text)
			}
		}
	}
	out := askAtNow(t, st, retrieve.Request{
		Project: "acme/app", Question: "what failed",
		Paths: []string{"ios/LightboxView.swift"},
	})
	if contextHas(out, "bundle step") {
		t.Fatalf("dist failed packed: %+v", out.Context)
	}
	if !contextHas(out, "Who-reacted") {
		t.Fatalf("product failed missed: %+v", out.Context)
	}
}

func testBOMJSONL(t *testing.T) {
	st := dogfoodStore(t)
	body := "\ufeff" + grokLine("assistant", "We decided to use jose, not jsonwebtoken, for Edge in src/middleware/auth.ts.")
	p := testdataWrite(t, t.TempDir(), "bom.jsonl", body)
	if _, err := write.CatchUp(st, write.CatchUpRequest{
		JSONL: p, Project: "acme/app", Harness: "grok", SessionID: "bom",
	}); err != nil {
		t.Fatal(err)
	}
	out := askAtNow(t, st, retrieve.Request{
		Project: "acme/app", Question: "jose", Paths: []string{"src/middleware/auth.ts"},
	})
	if !contextHas(out, "jose") {
		t.Fatalf("BOM jsonl missed gold: %+v", out.Context)
	}
}

func testClaimJSONArray(t *testing.T) {
	st := dogfoodStore(t)
	arr := `[{"id":"01JFAIL","type":"failed","text":"Redis token bucket failed in src/middleware/auth.ts staging."}]`
	var b strings.Builder
	b.WriteString(grokLine("assistant", arr))
	b.WriteString(grokLine("assistant", "We decided to use jose, not jsonwebtoken, for Edge in src/middleware/auth.ts."))
	dogfoodCatch(t, st, "acme/app", "arr", b.String())
	out := askAtNow(t, st, retrieve.Request{
		Project: "acme/app", Question: "jose", Paths: []string{"src/middleware/auth.ts"},
	})
	if contextHas(out, "Redis token bucket") {
		t.Fatalf("claim JSON array echoed: %+v", out.Context)
	}
	if !contextHas(out, "jose") {
		t.Fatalf("jose missed: %+v", out.Context)
	}
}

func testPercentEncodedPath(t *testing.T) {
	got := redact.FilterPaths([]string{
		"src/ok.go",
		"..%2f..%2fetc/passwd",
		"src/%2e%2e/etc/shadow",
		".git/hooks/pre-commit.sh",
		"keys/id_rsa.pub",
		".github/workflows/ci.yml",
	})
	if len(got) != 2 || got[0] != "src/ok.go" || got[1] != ".github/workflows/ci.yml" {
		t.Fatalf("%v", got)
	}
}

func testSameMessagePathBleed(t *testing.T) {
	st := dogfoodStore(t)
	dogfoodCatch(t, st, "acme/app", "bleedc",
		grokLine("user", "Always never log Authorization headers in src/middleware/auth.ts. Always never bikeshed the font in the settings panel."))
	claims, _ := st.ListActive("acme/app")
	for _, c := range claims {
		if strings.Contains(c.Text, "bikeshed") {
			for _, p := range c.Paths {
				if strings.Contains(p, "auth.ts") {
					t.Fatalf("pathless constraint inherited sibling path: %+v", c)
				}
			}
		}
	}
	out := askAtNow(t, st, retrieve.Request{
		Project: "acme/app", Question: "Authorization headers",
		Paths: []string{"src/middleware/auth.ts"},
	})
	if !contextHas(out, "Authorization") {
		t.Fatalf("real constraint missed: %+v", out.Context)
	}
	if contextHas(out, "bikeshed") {
		t.Fatalf("sibling constraint packed: %+v", out.Context)
	}
}

func testSameMessageFailedBleed(t *testing.T) {
	st := dogfoodStore(t)
	dogfoodCatch(t, st, "acme/app", "bleedf",
		grokLine("assistant", "We decided to use jose, not jsonwebtoken, for Edge in src/middleware/auth.ts. Helper decoy failed during compile."))
	claims, _ := st.ListActive("acme/app")
	for _, c := range claims {
		if !strings.Contains(c.Text, "Helper decoy") {
			continue
		}
		for _, p := range c.Paths {
			if strings.Contains(p, "auth.ts") {
				t.Fatalf("pathless decoy failed inherited sibling path: %+v", c)
			}
		}
	}
	out := askAtNow(t, st, retrieve.Request{
		Project: "acme/app", Question: "jose", Paths: []string{"src/middleware/auth.ts"},
	})
	if !contextHas(out, "jose") {
		t.Fatalf("jose missed: %+v", out.Context)
	}
}

func testContentObjectText(t *testing.T) {
	st := dogfoodStore(t)
	body := `{"role":"assistant","content":{"text":"We decided to use jose, not jsonwebtoken, for Edge in src/middleware/auth.ts."}}` + "\n"
	p := testdataWrite(t, t.TempDir(), "obj.jsonl", body)
	if _, err := write.CatchUp(st, write.CatchUpRequest{
		JSONL: p, Project: "acme/app", Harness: "grok", SessionID: "obj",
	}); err != nil {
		t.Fatal(err)
	}
	out := askAtNow(t, st, retrieve.Request{
		Project: "acme/app", Question: "jose", Paths: []string{"src/middleware/auth.ts"},
	})
	if !contextHas(out, "jose") {
		t.Fatalf("content object missed: %+v", out.Context)
	}
}

func testHumanRole(t *testing.T) {
	st := dogfoodStore(t)
	body := `{"role":"Human","content":"Always never log Authorization headers in src/middleware/auth.ts."}` + "\n" +
		`{"role":"Assistant","content":"We decided to use jose, not jsonwebtoken, for Edge in src/middleware/auth.ts."}` + "\n"
	p := testdataWrite(t, t.TempDir(), "human.jsonl", body)
	if _, err := write.CatchUp(st, write.CatchUpRequest{
		JSONL: p, Project: "acme/app", Harness: "grok", SessionID: "human",
	}); err != nil {
		t.Fatal(err)
	}
	out := askAtNow(t, st, retrieve.Request{
		Project: "acme/app", Question: "Authorization headers",
		Paths: []string{"src/middleware/auth.ts"},
	})
	if !contextHas(out, "Authorization") {
		t.Fatalf("Human role constraint missed: %+v", out.Context)
	}
	if !contextHas(out, "jose") {
		t.Fatalf("Assistant role decision missed: %+v", out.Context)
	}
}

func testDoesntWork(t *testing.T) {
	st := dogfoodStore(t)
	var b strings.Builder
	b.WriteString(grokLine("assistant", "The limiter doesn't work in src/middleware/auth.ts after the pool change."))
	b.WriteString(grokLine("assistant", "The limiter doesn’t work in src/middleware/auth.ts on Edge."))
	dogfoodCatch(t, st, "acme/app", "doesnt", b.String())
	out := askAtNow(t, st, retrieve.Request{
		Project: "acme/app", Goal: "add rate limiting",
		Paths: []string{"src/middleware/auth.ts"},
	})
	if !contextHas(out, "doesn't work") && !contextHas(out, "doesn’t work") {
		t.Fatalf("doesn't-work missed: %+v", out.Context)
	}
}

func testWindowsSepPath(t *testing.T) {
	st := dogfoodStore(t)
	dogfoodCatch(t, st, "acme/app", "winsep",
		grokLine("assistant", `We decided to use jose, not jsonwebtoken, for Edge in src\middleware\auth.ts.`))
	claims, _ := st.ListActive("acme/app")
	found := false
	for _, c := range claims {
		if !strings.Contains(c.Text, "jose") {
			continue
		}
		found = true
		ok := false
		for _, p := range c.Paths {
			if strings.ReplaceAll(p, "\\", "/") == "src/middleware/auth.ts" {
				ok = true
			}
		}
		if !ok {
			t.Fatalf("windows sep path not normalized: %+v", c)
		}
	}
	if !found {
		t.Fatal("jose decision missed")
	}
}

func testDotSlashPath(t *testing.T) {
	st := dogfoodStore(t)
	dogfoodCatch(t, st, "acme/app", "dotsl",
		grokLine("assistant", "We decided to use jose, not jsonwebtoken, for Edge in ./src/middleware/auth.ts."))
	claims, _ := st.ListActive("acme/app")
	for _, c := range claims {
		for _, p := range c.Paths {
			if strings.HasPrefix(p, "./") {
				t.Fatalf("./ prefix kept: %+v", c)
			}
		}
	}
	out := askAtNow(t, st, retrieve.Request{
		Project: "acme/app", Question: "jose", Paths: []string{"src/middleware/auth.ts"},
	})
	if !contextHas(out, "jose") {
		t.Fatalf("dot-slash gold missed: %+v", out.Context)
	}
}

func testAgentReminder(t *testing.T) {
	st := dogfoodStore(t)
	text := "Always never log Authorization headers in src/middleware/auth.ts.\n" +
		"<agent-reminder>\nAlways never use the simulator. Don't change source.\n</agent-reminder>"
	dogfoodCatch(t, st, "acme/app", "agrem", grokLine("user", text))
	out := askAtNow(t, st, retrieve.Request{
		Project: "acme/app", Question: "Authorization headers",
		Paths: []string{"src/middleware/auth.ts"},
	})
	if !contextHas(out, "Authorization") {
		t.Fatalf("real constraint missed: %+v", out.Context)
	}
	if contextHasAny(out, "simulator", "Don't change source") {
		t.Fatalf("agent-reminder packed: %+v", out.Context)
	}
}

func testJestFail(t *testing.T) {
	st := dogfoodStore(t)
	var b strings.Builder
	b.WriteString(grokLine("assistant", "FAIL src/middleware/auth.test.ts"))
	b.WriteString(grokLine("assistant", "FAIL  src/other/file.test.ts"))
	b.WriteString(grokLine("assistant", "Who-reacted failed in preview in ios/LightboxView.swift."))
	dogfoodCatch(t, st, "acme/app", "jest", b.String())
	out := askAtNow(t, st, retrieve.Request{
		Project: "acme/app", Question: "what failed",
		Paths: []string{"ios/LightboxView.swift"},
	})
	if contextHasAny(out, "auth.test.ts", "file.test.ts") {
		t.Fatalf("jest FAIL packed: %+v", out.Context)
	}
	if !contextHas(out, "Who-reacted") {
		t.Fatalf("product failed missed: %+v", out.Context)
	}
}

func testIllRewrite(t *testing.T) {
	st := dogfoodStore(t)
	var b strings.Builder
	b.WriteString(grokLine("assistant", "I'll rewrite the limiter instead of Redis in src/middleware/auth.ts."))
	b.WriteString(grokLine("assistant", "We decided to use jose, not jsonwebtoken, for Edge in src/middleware/auth.ts."))
	dogfoodCatch(t, st, "acme/app", "rewr", b.String())
	claims, _ := st.ListActive("acme/app")
	for _, c := range claims {
		if strings.Contains(c.Text, "I'll rewrite") {
			t.Fatalf("planning rewrite extracted: %+v", c)
		}
	}
	out := askAtNow(t, st, retrieve.Request{
		Project: "acme/app", Question: "jose", Paths: []string{"src/middleware/auth.ts"},
	})
	if !contextHas(out, "jose") {
		t.Fatalf("jose missed: %+v", out.Context)
	}
}

func testWeDontHaveTime(t *testing.T) {
	st := dogfoodStore(t)
	var b strings.Builder
	b.WriteString(grokLine("user", "Always never log Authorization headers in src/middleware/auth.ts."))
	b.WriteString(grokLine("user", "We don't have time to bikeshed src/middleware/auth.ts today."))
	dogfoodCatch(t, st, "acme/app", "wdht", b.String())
	claims, _ := st.ListActive("acme/app")
	for _, c := range claims {
		if strings.Contains(c.Text, "have time") {
			t.Fatalf("session chatter extracted: %+v", c)
		}
	}
	out := askAtNow(t, st, retrieve.Request{
		Project: "acme/app", Question: "Authorization headers",
		Paths: []string{"src/middleware/auth.ts"},
	})
	if !contextHas(out, "Authorization") {
		t.Fatalf("real constraint missed: %+v", out.Context)
	}
}

func testCRLFJSONL(t *testing.T) {
	st := dogfoodStore(t)
	line := grokLine("assistant", "We decided to use jose, not jsonwebtoken, for Edge in src/middleware/auth.ts.")
	body := strings.ReplaceAll(line, "\n", "\r\n")
	p := testdataWrite(t, t.TempDir(), "crlf.jsonl", body)
	if _, err := write.CatchUp(st, write.CatchUpRequest{
		JSONL: p, Project: "acme/app", Harness: "grok", SessionID: "crlf",
	}); err != nil {
		t.Fatal(err)
	}
	out := askAtNow(t, st, retrieve.Request{
		Project: "acme/app", Question: "jose", Paths: []string{"src/middleware/auth.ts"},
	})
	if !contextHas(out, "jose") {
		t.Fatalf("CRLF jsonl missed: %+v", out.Context)
	}
}

func testEnvrcTarget(t *testing.T) {
	got := redact.FilterPaths([]string{
		"src/ok.go",
		".envrc",
		"target/debug/app",
		"Pods/Alamofire/Alamofire.swift",
	})
	if len(got) != 1 || got[0] != "src/ok.go" {
		t.Fatalf("%v", got)
	}
}

func testSystemRole(t *testing.T) {
	st := dogfoodStore(t)
	body := `{"role":"System","content":"Always never use mongo in src/db/client.ts. We decided to use mongo, not postgres, in src/db/client.ts."}` + "\n" +
		`{"role":"assistant","content":"We decided to use jose, not jsonwebtoken, for Edge in src/middleware/auth.ts."}` + "\n"
	p := testdataWrite(t, t.TempDir(), "sys.jsonl", body)
	if _, err := write.CatchUp(st, write.CatchUpRequest{
		JSONL: p, Project: "acme/app", Harness: "grok", SessionID: "sys",
	}); err != nil {
		t.Fatal(err)
	}
	claims, _ := st.ListActive("acme/app")
	for _, c := range claims {
		if strings.Contains(c.Text, "mongo") {
			t.Fatalf("system role extracted: %+v", c)
		}
	}
	out := askAtNow(t, st, retrieve.Request{
		Project: "acme/app", Question: "jose", Paths: []string{"src/middleware/auth.ts"},
	})
	if !contextHas(out, "jose") {
		t.Fatalf("jose missed: %+v", out.Context)
	}
}

func testHTMLComment(t *testing.T) {
	st := dogfoodStore(t)
	var b strings.Builder
	b.WriteString(grokLine("assistant", "<!-- We decided to use mongo, not postgres, in src/db/client.ts. -->"))
	b.WriteString(grokLine("assistant", "We decided to use jose, not jsonwebtoken, for Edge in src/middleware/auth.ts."))
	dogfoodCatch(t, st, "acme/app", "htmlc", b.String())
	claims, _ := st.ListActive("acme/app")
	for _, c := range claims {
		if strings.Contains(c.Text, "mongo") {
			t.Fatalf("html comment extracted: %+v", c)
		}
	}
	out := askAtNow(t, st, retrieve.Request{
		Project: "acme/app", Question: "jose", Paths: []string{"src/middleware/auth.ts"},
	})
	if !contextHas(out, "jose") {
		t.Fatalf("jose missed: %+v", out.Context)
	}
}

func testContentParts(t *testing.T) {
	st := dogfoodStore(t)
	body := `{"role":"assistant","content":{"parts":[{"text":"We decided to use jose, not jsonwebtoken, for Edge in src/middleware/auth.ts."}]}}` + "\n"
	p := testdataWrite(t, t.TempDir(), "parts.jsonl", body)
	if _, err := write.CatchUp(st, write.CatchUpRequest{
		JSONL: p, Project: "acme/app", Harness: "grok", SessionID: "parts",
	}); err != nil {
		t.Fatal(err)
	}
	out := askAtNow(t, st, retrieve.Request{
		Project: "acme/app", Question: "jose", Paths: []string{"src/middleware/auth.ts"},
	})
	if !contextHas(out, "jose") {
		t.Fatalf("content.parts missed: %+v", out.Context)
	}
}

func testNestedBashTool(t *testing.T) {
	st := dogfoodStore(t)
	body := strings.Join([]string{
		`{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":"Redis token bucket failed in src/middleware/auth.ts staging.\n--- FAIL: TestLimiter"}]}}`,
		`{"type":"assistant","content":"We decided to use jose, not jsonwebtoken, for Edge in src/middleware/auth.ts."}`,
	}, "\n") + "\n"
	p := testdataWrite(t, t.TempDir(), "bash.jsonl", body)
	if _, err := write.CatchUp(st, write.CatchUpRequest{
		JSONL: p, Project: "acme/app", Harness: "claude", SessionID: "bash",
	}); err != nil {
		t.Fatal(err)
	}
	out := askAtNow(t, st, retrieve.Request{
		Project: "acme/app", Question: "jose", Paths: []string{"src/middleware/auth.ts"},
	})
	if contextHas(out, "Redis token bucket") {
		t.Fatalf("nested bash tool_result extracted: %+v", out.Context)
	}
	if !contextHas(out, "jose") {
		t.Fatalf("jose missed: %+v", out.Context)
	}
}

func testIllMigrate(t *testing.T) {
	st := dogfoodStore(t)
	var b strings.Builder
	b.WriteString(grokLine("assistant", "I'll migrate the limiter instead of Redis in src/middleware/auth.ts."))
	b.WriteString(grokLine("assistant", "We decided to use jose, not jsonwebtoken, for Edge in src/middleware/auth.ts."))
	dogfoodCatch(t, st, "acme/app", "mig", b.String())
	claims, _ := st.ListActive("acme/app")
	for _, c := range claims {
		if strings.Contains(c.Text, "I'll migrate") {
			t.Fatalf("planning migrate extracted: %+v", c)
		}
	}
	if !contextHas(askAtNow(t, st, retrieve.Request{
		Project: "acme/app", Question: "jose", Paths: []string{"src/middleware/auth.ts"},
	}), "jose") {
		t.Fatal("jose missed")
	}
}

func testClaudeUserContext(t *testing.T) {
	st := dogfoodStore(t)
	text := "Always never log Authorization headers in src/middleware/auth.ts.\n" +
		"<claude-user-context>\nAlways never use the simulator.\n</claude-user-context>"
	dogfoodCatch(t, st, "acme/app", "cuc", grokLine("user", text))
	out := askAtNow(t, st, retrieve.Request{
		Project: "acme/app", Question: "Authorization headers",
		Paths: []string{"src/middleware/auth.ts"},
	})
	if !contextHas(out, "Authorization") {
		t.Fatalf("real constraint missed: %+v", out.Context)
	}
	if contextHas(out, "simulator") {
		t.Fatalf("claude-user-context packed: %+v", out.Context)
	}
}

func testCredentialsJSON(t *testing.T) {
	got := redact.FilterPaths([]string{"src/ok.go", "secrets/credentials.json", "aws-exports.js"})
	if len(got) != 1 || got[0] != "src/ok.go" {
		t.Fatalf("%v", got)
	}
}

func testChoseWrong(t *testing.T) {
	st := dogfoodStore(t)
	var b strings.Builder
	b.WriteString(grokLine("assistant", "I chose the wrong approach in src/middleware/auth.ts last night."))
	b.WriteString(grokLine("assistant", "We decided to use jose, not jsonwebtoken, for Edge in src/middleware/auth.ts."))
	dogfoodCatch(t, st, "acme/app", "chose", b.String())
	claims, _ := st.ListActive("acme/app")
	for _, c := range claims {
		if strings.Contains(c.Text, "wrong approach") {
			t.Fatalf("narrative chose extracted: %+v", c)
		}
	}
	if !contextHas(askAtNow(t, st, retrieve.Request{
		Project: "acme/app", Question: "jose", Paths: []string{"src/middleware/auth.ts"},
	}), "jose") {
		t.Fatal("jose missed")
	}
}

func testLeadingWS(t *testing.T) {
	st := dogfoodStore(t)
	line := grokLine("assistant", "We decided to use jose, not jsonwebtoken, for Edge in src/middleware/auth.ts.")
	body := "   " + line
	p := testdataWrite(t, t.TempDir(), "ws.jsonl", body)
	if _, err := write.CatchUp(st, write.CatchUpRequest{
		JSONL: p, Project: "acme/app", Harness: "grok", SessionID: "ws",
	}); err != nil {
		t.Fatal(err)
	}
	out := askAtNow(t, st, retrieve.Request{
		Project: "acme/app", Question: "jose", Paths: []string{"src/middleware/auth.ts"},
	})
	if !contextHas(out, "jose") {
		t.Fatalf("leading-ws jsonl missed: %+v", out.Context)
	}
}

func testMustBeABug(t *testing.T) {
	st := dogfoodStore(t)
	var b strings.Builder
	b.WriteString(grokLine("user", "Always never log Authorization headers in src/middleware/auth.ts."))
	b.WriteString(grokLine("user", "Must be a bug in src/middleware/auth.ts then."))
	dogfoodCatch(t, st, "acme/app", "mustbug", b.String())
	claims, _ := st.ListActive("acme/app")
	for _, c := range claims {
		if strings.Contains(c.Text, "Must be a bug") {
			t.Fatalf("must-be-a-bug extracted: %+v", c)
		}
	}
	if !contextHas(askAtNow(t, st, retrieve.Request{
		Project: "acme/app", Question: "Authorization headers",
		Paths: []string{"src/middleware/auth.ts"},
	}), "Authorization") {
		t.Fatal("real constraint missed")
	}
}

func testDeveloperRole(t *testing.T) {
	st := dogfoodStore(t)
	body := `{"role":"Developer","content":"We decided to use mongo, not postgres, in src/db/client.ts."}` + "\n" +
		`{"role":"assistant","content":"We decided to use jose, not jsonwebtoken, for Edge in src/middleware/auth.ts."}` + "\n"
	p := testdataWrite(t, t.TempDir(), "dev.jsonl", body)
	if _, err := write.CatchUp(st, write.CatchUpRequest{
		JSONL: p, Project: "acme/app", Harness: "grok", SessionID: "dev",
	}); err != nil {
		t.Fatal(err)
	}
	claims, _ := st.ListActive("acme/app")
	for _, c := range claims {
		if strings.Contains(c.Text, "mongo") {
			t.Fatalf("developer role extracted: %+v", c)
		}
	}
	if !contextHas(askAtNow(t, st, retrieve.Request{
		Project: "acme/app", Question: "jose", Paths: []string{"src/middleware/auth.ts"},
	}), "jose") {
		t.Fatal("jose missed")
	}
}

func testPluginPrefixedAsk(t *testing.T) {
	st := dogfoodStore(t)
	body := strings.Join([]string{
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","id":"a1","name":"mcp__plugin_lossless__ask","input":{}}]}}`,
		`{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"a1","content":"{\"context\":[{\"id\":\"x\",\"type\":\"failed\",\"text\":\"Redis token bucket failed in src/middleware/auth.ts staging.\"}],\"warnings\":[\"A prior attempt failed\"],\"tokens\":4,\"project\":\"acme/app\"}"}]}}`,
		`{"type":"assistant","content":"We decided to use jose, not jsonwebtoken, for Edge in src/middleware/auth.ts."}`,
	}, "\n") + "\n"
	p := testdataWrite(t, t.TempDir(), "plug.jsonl", body)
	if _, err := write.CatchUp(st, write.CatchUpRequest{
		JSONL: p, Project: "acme/app", Harness: "claude", SessionID: "plug",
	}); err != nil {
		t.Fatal(err)
	}
	out := askAtNow(t, st, retrieve.Request{
		Project: "acme/app", Question: "jose", Paths: []string{"src/middleware/auth.ts"},
	})
	if contextHas(out, "Redis token bucket") {
		t.Fatalf("plugin-prefixed ask echoed: %+v", out.Context)
	}
	if !contextHas(out, "jose") {
		t.Fatalf("jose missed: %+v", out.Context)
	}
}

func testAlmostPicked(t *testing.T) {
	st := dogfoodStore(t)
	var b strings.Builder
	b.WriteString(grokLine("assistant", "I almost picked mongo over postgres in src/db/client.ts."))
	b.WriteString(grokLine("assistant", "We decided to use jose, not jsonwebtoken, for Edge in src/middleware/auth.ts."))
	dogfoodCatch(t, st, "acme/app", "almost", b.String())
	claims, _ := st.ListActive("acme/app")
	for _, c := range claims {
		if strings.Contains(c.Text, "mongo") {
			t.Fatalf("almost-picked extracted: %+v", c)
		}
	}
	if !contextHas(askAtNow(t, st, retrieve.Request{
		Project: "acme/app", Question: "jose", Paths: []string{"src/middleware/auth.ts"},
	}), "jose") {
		t.Fatal("jose missed")
	}
}

func testOutputTextParts(t *testing.T) {
	st := dogfoodStore(t)
	body := `{"role":"assistant","content":[{"type":"output_text","text":"We decided to use jose, not jsonwebtoken, for Edge in src/middleware/auth.ts."}]}` + "\n"
	p := testdataWrite(t, t.TempDir(), "outp.jsonl", body)
	if _, err := write.CatchUp(st, write.CatchUpRequest{
		JSONL: p, Project: "acme/app", Harness: "grok", SessionID: "outp",
	}); err != nil {
		t.Fatal(err)
	}
	if !contextHas(askAtNow(t, st, retrieve.Request{
		Project: "acme/app", Question: "jose", Paths: []string{"src/middleware/auth.ts"},
	}), "jose") {
		t.Fatal("output_text parts missed")
	}
}

func testTabPrefixedJSONL(t *testing.T) {
	st := dogfoodStore(t)
	line := grokLine("assistant", "We decided to use jose, not jsonwebtoken, for Edge in src/middleware/auth.ts.")
	p := testdataWrite(t, t.TempDir(), "tab.jsonl", "\t"+line)
	if _, err := write.CatchUp(st, write.CatchUpRequest{
		JSONL: p, Project: "acme/app", Harness: "grok", SessionID: "tab",
	}); err != nil {
		t.Fatal(err)
	}
	if !contextHas(askAtNow(t, st, retrieve.Request{
		Project: "acme/app", Question: "jose", Paths: []string{"src/middleware/auth.ts"},
	}), "jose") {
		t.Fatal("tab-prefixed jsonl missed")
	}
}

func testDotPaths(t *testing.T) {
	got := redact.FilterPaths([]string{"src/ok.go", ".", "..", "./", "src/ok.go/"})
	if len(got) < 1 || got[0] != "src/ok.go" {
		t.Fatalf("%v", got)
	}
	for _, p := range got {
		if p == "." || p == ".." || p == "" {
			t.Fatalf("dot path kept: %v", got)
		}
	}
}

func testCustomMessage(t *testing.T) {
	st := dogfoodStore(t)
	body := `{"type":"custom_message","content":"We decided to use mongo, not postgres, in src/db/client.ts."}` + "\n" +
		`{"type":"last-prompt","content":"We decided to use mongo, not postgres, in src/db/client.ts."}` + "\n" +
		`{"role":"assistant","content":"We decided to use jose, not jsonwebtoken, for Edge in src/middleware/auth.ts."}` + "\n"
	p := testdataWrite(t, t.TempDir(), "cust.jsonl", body)
	if _, err := write.CatchUp(st, write.CatchUpRequest{
		JSONL: p, Project: "acme/app", Harness: "grok", SessionID: "cust",
	}); err != nil {
		t.Fatal(err)
	}
	claims, _ := st.ListActive("acme/app")
	for _, c := range claims {
		if strings.Contains(c.Text, "mongo") {
			t.Fatalf("custom/last-prompt extracted: %+v", c)
		}
	}
}

func testYAMLAskPacket(t *testing.T) {
	st := dogfoodStore(t)
	yaml := "context:\n- type: failed\n  text: Redis token bucket failed in src/middleware/auth.ts staging.\nwarnings:\n- A prior attempt failed\n"
	var b strings.Builder
	b.WriteString(grokLine("assistant", yaml))
	b.WriteString(grokLine("assistant", "We decided to use jose, not jsonwebtoken, for Edge in src/middleware/auth.ts."))
	dogfoodCatch(t, st, "acme/app", "yaml", b.String())
	out := askAtNow(t, st, retrieve.Request{
		Project: "acme/app", Question: "jose", Paths: []string{"src/middleware/auth.ts"},
	})
	if contextHas(out, "Redis token bucket") {
		t.Fatalf("yaml ask packet echoed: %+v", out.Context)
	}
	if !contextHas(out, "jose") {
		t.Fatalf("jose missed: %+v", out.Context)
	}
}

func testUserDecisionKept(t *testing.T) {
	st := dogfoodStore(t)
	dogfoodCatch(t, st, "acme/app", "ud",
		grokLine("user", "We decided to use jose, not jsonwebtoken, for Edge in src/middleware/auth.ts."))
	out := askAtNow(t, st, retrieve.Request{
		Project: "acme/app", Question: "jose", Paths: []string{"src/middleware/auth.ts"},
	})
	if !contextHas(out, "jose") {
		t.Fatalf("user decision missed: %+v", out.Context)
	}
}

func testTOMLAskPacket(t *testing.T) {
	st := dogfoodStore(t)
	toml := "[[context]]\ntype = \"failed\"\ntext = \"Redis token bucket failed in src/middleware/auth.ts staging.\"\n"
	var b strings.Builder
	b.WriteString(grokLine("assistant", toml))
	b.WriteString(grokLine("assistant", "We decided to use jose, not jsonwebtoken, for Edge in src/middleware/auth.ts."))
	dogfoodCatch(t, st, "acme/app", "toml", b.String())
	out := askAtNow(t, st, retrieve.Request{
		Project: "acme/app", Question: "jose", Paths: []string{"src/middleware/auth.ts"},
	})
	if contextHas(out, "Redis token bucket") {
		t.Fatalf("toml ask packet echoed: %+v", out.Context)
	}
	if !contextHas(out, "jose") {
		t.Fatalf("jose missed: %+v", out.Context)
	}
}

func testChatML(t *testing.T) {
	st := dogfoodStore(t)
	text := "<|im_start|>assistant\nWe decided to use jose, not jsonwebtoken, for Edge in src/middleware/auth.ts.\n<|im_end|>"
	dogfoodCatch(t, st, "acme/app", "chatml", grokLine("assistant", text))
	out := askAtNow(t, st, retrieve.Request{
		Project: "acme/app", Question: "jose", Paths: []string{"src/middleware/auth.ts"},
	})
	if !contextHas(out, "jose") {
		t.Fatalf("chatml missed: %+v", out.Context)
	}
}

func testGitConflict(t *testing.T) {
	st := dogfoodStore(t)
	var b strings.Builder
	b.WriteString(grokLine("assistant", "<<<<<<< HEAD\nWe decided to use mongo, not postgres, in src/db/client.ts.\n=======\nWe decided to use jose, not jsonwebtoken, for Edge in src/middleware/auth.ts.\n>>>>>>> main"))
	dogfoodCatch(t, st, "acme/app", "cflt", b.String())
	claims, _ := st.ListActive("acme/app")
	for _, c := range claims {
		if strings.Contains(c.Text, "mongo") || strings.HasPrefix(strings.TrimSpace(c.Text), "<<<<<<") {
			t.Fatalf("conflict marker extracted: %+v", c)
		}
	}
	out := askAtNow(t, st, retrieve.Request{
		Project: "acme/app", Question: "jose", Paths: []string{"src/middleware/auth.ts"},
	})
	if !contextHas(out, "jose") {
		t.Fatalf("jose side of conflict missed: %+v", out.Context)
	}
}

func testUpperUSER(t *testing.T) {
	st := dogfoodStore(t)
	body := `{"role":"USER","content":"Always never log Authorization headers in src/middleware/auth.ts."}` + "\n"
	p := testdataWrite(t, t.TempDir(), "USER.jsonl", body)
	if _, err := write.CatchUp(st, write.CatchUpRequest{
		JSONL: p, Project: "acme/app", Harness: "grok", SessionID: "USER",
	}); err != nil {
		t.Fatal(err)
	}
	if !contextHas(askAtNow(t, st, retrieve.Request{
		Project: "acme/app", Question: "Authorization headers",
		Paths: []string{"src/middleware/auth.ts"},
	}), "Authorization") {
		t.Fatal("USER role constraint missed")
	}
}

func testNullByteLine(t *testing.T) {
	st := dogfoodStore(t)
	bad := "{\"role\":\"assistant\",\"content\":\"We decided to use mongo\\u0000 in src/db/client.ts.\"}\n"
	good := grokLine("assistant", "We decided to use jose, not jsonwebtoken, for Edge in src/middleware/auth.ts.")
	p := testdataWrite(t, t.TempDir(), "nul.jsonl", bad+good)
	if _, err := write.CatchUp(st, write.CatchUpRequest{
		JSONL: p, Project: "acme/app", Harness: "grok", SessionID: "nul",
	}); err != nil {
		t.Fatal(err)
	}
	if !contextHas(askAtNow(t, st, retrieve.Request{
		Project: "acme/app", Question: "jose", Paths: []string{"src/middleware/auth.ts"},
	}), "jose") {
		t.Fatal("gold after null-byte line missed")
	}
}

func testINIAskPacket(t *testing.T) {
	st := dogfoodStore(t)
	ini := "[context]\ntype=failed\ntext=Redis token bucket failed in src/middleware/auth.ts staging.\n"
	var b strings.Builder
	b.WriteString(grokLine("assistant", ini))
	b.WriteString(grokLine("assistant", "We decided to use jose, not jsonwebtoken, for Edge in src/middleware/auth.ts."))
	dogfoodCatch(t, st, "acme/app", "ini", b.String())
	out := askAtNow(t, st, retrieve.Request{
		Project: "acme/app", Question: "jose", Paths: []string{"src/middleware/auth.ts"},
	})
	if contextHas(out, "Redis token bucket") {
		t.Fatalf("ini ask packet echoed: %+v", out.Context)
	}
	if !contextHas(out, "jose") {
		t.Fatalf("jose missed: %+v", out.Context)
	}
}

func testWeWillUseHour(t *testing.T) {
	st := dogfoodStore(t)
	var b strings.Builder
	b.WriteString(grokLine("assistant", "We will use the next hour to inspect src/middleware/auth.ts."))
	b.WriteString(grokLine("assistant", "We decided to use jose, not jsonwebtoken, for Edge in src/middleware/auth.ts."))
	dogfoodCatch(t, st, "acme/app", "hour", b.String())
	claims, _ := st.ListActive("acme/app")
	for _, c := range claims {
		if strings.Contains(c.Text, "next hour") {
			t.Fatalf("we-will-use-hour extracted: %+v", c)
		}
	}
	if !contextHas(askAtNow(t, st, retrieve.Request{
		Project: "acme/app", Question: "jose", Paths: []string{"src/middleware/auth.ts"},
	}), "jose") {
		t.Fatal("jose missed")
	}
}

func testDiffHunk(t *testing.T) {
	st := dogfoodStore(t)
	var b strings.Builder
	b.WriteString(grokLine("assistant", "--- a/src/middleware/auth.ts\n+++ b/src/middleware/auth.ts\n- Redis token bucket failed in src/middleware/auth.ts staging.\n+ keep jose"))
	b.WriteString(grokLine("assistant", "Who-reacted failed in preview in ios/LightboxView.swift."))
	dogfoodCatch(t, st, "acme/app", "diff", b.String())
	out := askAtNow(t, st, retrieve.Request{
		Project: "acme/app", Question: "what failed",
		Paths: []string{"ios/LightboxView.swift"},
	})
	if contextHas(out, "Redis token bucket") {
		t.Fatalf("diff hunk packed: %+v", out.Context)
	}
	if !contextHas(out, "Who-reacted") {
		t.Fatalf("product failed missed: %+v", out.Context)
	}
}

func testCurlyDontWait(t *testing.T) {
	st := dogfoodStore(t)
	var b strings.Builder
	b.WriteString(grokLine("user", "Always never log Authorization headers in src/middleware/auth.ts."))
	b.WriteString(grokLine("user", "Don’t wait for me, just keep going on src/middleware/auth.ts."))
	dogfoodCatch(t, st, "acme/app", "cdw", b.String())
	claims, _ := st.ListActive("acme/app")
	for _, c := range claims {
		if strings.Contains(c.Text, "wait for me") {
			t.Fatalf("curly don't-wait extracted: %+v", c)
		}
	}
	if !contextHas(askAtNow(t, st, retrieve.Request{
		Project: "acme/app", Question: "Authorization headers",
		Paths: []string{"src/middleware/auth.ts"},
	}), "Authorization") {
		t.Fatal("real constraint missed")
	}
}

func testModelRoleUpper(t *testing.T) {
	st := dogfoodStore(t)
	body := `{"role":"MODEL","content":"We decided to use jose, not jsonwebtoken, for Edge in src/middleware/auth.ts."}` + "\n"
	p := testdataWrite(t, t.TempDir(), "MODEL.jsonl", body)
	if _, err := write.CatchUp(st, write.CatchUpRequest{
		JSONL: p, Project: "acme/app", Harness: "grok", SessionID: "MODEL",
	}); err != nil {
		t.Fatal(err)
	}
	if !contextHas(askAtNow(t, st, retrieve.Request{
		Project: "acme/app", Question: "jose", Paths: []string{"src/middleware/auth.ts"},
	}), "jose") {
		t.Fatal("MODEL role missed")
	}
}

func testVenvPath(t *testing.T) {
	got := redact.FilterPaths([]string{"src/ok.go", ".venv/lib/foo.py", "venv/lib/foo.py", "lib/site-packages/foo.py"})
	if len(got) != 1 || got[0] != "src/ok.go" {
		t.Fatalf("%v", got)
	}
}

func testPrettyAskJSON(t *testing.T) {
	st := dogfoodStore(t)
	pretty := "{\n  \"context\": [{\"id\":\"x\",\"type\":\"failed\",\"text\":\"Redis token bucket failed in src/middleware/auth.ts staging.\"}],\n  \"warnings\": [\"A prior attempt failed\"],\n  \"tokens\": 8,\n  \"project\": \"acme/app\"\n}"
	var b strings.Builder
	b.WriteString(grokLine("assistant", pretty))
	b.WriteString(grokLine("assistant", "We decided to use jose, not jsonwebtoken, for Edge in src/middleware/auth.ts."))
	dogfoodCatch(t, st, "acme/app", "pretty", b.String())
	out := askAtNow(t, st, retrieve.Request{
		Project: "acme/app", Question: "jose", Paths: []string{"src/middleware/auth.ts"},
	})
	if contextHas(out, "Redis token bucket") {
		t.Fatalf("pretty ask JSON echoed: %+v", out.Context)
	}
	if !contextHas(out, "jose") {
		t.Fatalf("jose missed: %+v", out.Context)
	}
}

func testRedactedThenGold(t *testing.T) {
	st := dogfoodStore(t)
	body := "{\"_redacted\":true}\n" + grokLine("assistant", "We decided to use jose, not jsonwebtoken, for Edge in src/middleware/auth.ts.")
	p := testdataWrite(t, t.TempDir(), "red.jsonl", body)
	if _, err := write.CatchUp(st, write.CatchUpRequest{
		JSONL: p, Project: "acme/app", Harness: "grok", SessionID: "red",
	}); err != nil {
		t.Fatal(err)
	}
	if !contextHas(askAtNow(t, st, retrieve.Request{
		Project: "acme/app", Question: "jose", Paths: []string{"src/middleware/auth.ts"},
	}), "jose") {
		t.Fatal("gold after redacted line missed")
	}
}

func testIllRefactor(t *testing.T) {
	st := dogfoodStore(t)
	var b strings.Builder
	b.WriteString(grokLine("assistant", "I'll refactor the limiter instead of Redis in src/middleware/auth.ts."))
	b.WriteString(grokLine("assistant", "We decided to use jose, not jsonwebtoken, for Edge in src/middleware/auth.ts."))
	dogfoodCatch(t, st, "acme/app", "ref", b.String())
	claims, _ := st.ListActive("acme/app")
	for _, c := range claims {
		if strings.Contains(c.Text, "I'll refactor") {
			t.Fatalf("planning refactor extracted: %+v", c)
		}
	}
}

func testEmptyObject(t *testing.T) {
	st := dogfoodStore(t)
	body := "{}\n" + grokLine("assistant", "We decided to use jose, not jsonwebtoken, for Edge in src/middleware/auth.ts.")
	p := testdataWrite(t, t.TempDir(), "empty.jsonl", body)
	if _, err := write.CatchUp(st, write.CatchUpRequest{
		JSONL: p, Project: "acme/app", Harness: "grok", SessionID: "empty",
	}); err != nil {
		t.Fatal(err)
	}
	if !contextHas(askAtNow(t, st, retrieve.Request{
		Project: "acme/app", Question: "jose", Paths: []string{"src/middleware/auth.ts"},
	}), "jose") {
		t.Fatal("gold after empty object missed")
	}
}

func testNonJSONLine(t *testing.T) {
	st := dogfoodStore(t)
	body := "// not json\n" + grokLine("assistant", "We decided to use jose, not jsonwebtoken, for Edge in src/middleware/auth.ts.")
	p := testdataWrite(t, t.TempDir(), "njson.jsonl", body)
	if _, err := write.CatchUp(st, write.CatchUpRequest{
		JSONL: p, Project: "acme/app", Harness: "grok", SessionID: "njson",
	}); err != nil {
		t.Fatal(err)
	}
	if !contextHas(askAtNow(t, st, retrieve.Request{
		Project: "acme/app", Question: "jose", Paths: []string{"src/middleware/auth.ts"},
	}), "jose") {
		t.Fatal("gold after non-json line missed")
	}
}

func TestDogfoodRetryJoseStillPacks(t *testing.T) {
	st := dogfoodStore(t)
	src := testdataWrite(t, t.TempDir(), "grok-redis-retry.jsonl",
		mustRead(t, filepath.Join("..", "testdata", "bench", "sessions", "grok-redis-retry.jsonl")))
	if _, err := write.CatchUp(st, write.CatchUpRequest{
		JSONL: src, Project: "acme/api", Harness: "grok", SessionID: "grok-redis-retry",
	}); err != nil {
		t.Fatal(err)
	}
	claims, _ := st.ListActive("acme/api")
	var texts []string
	for _, c := range claims {
		texts = append(texts, c.Type+":"+c.Text)
	}
	t.Logf("extracted %d: %s", len(claims), strings.Join(texts, " || "))
	out := askAtNow(t, st, retrieve.Request{
		Project: "acme/api", Goal: "add rate limiting",
		Paths: []string{"src/middleware/auth.ts"},
	})
	t.Logf("context: %+v", out.Context)
	if !contextHas(out, "jose") {
		t.Fatalf("jose missing")
	}
}

func mustRead(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestDogfoodFilterPathsAttackSurface(t *testing.T) {
	got := redact.FilterPaths([]string{
		"src/ok.go",
		".github/workflows/ci.yml",
		"git.memora.pics/memora/memora.git",
		"https://github.com/acme/api.git",
		"Users/jay/secret.md",
		"etc/passwd",
		"keys/id_ed25519",
		"foo/authorized_keys",
		"src/../../.ssh/id_rsa",
		".git/hooks/pre-commit.sh",
		"keys/id_rsa.pub",
		"..%2fetc/passwd",
		"node_modules/lodash/index.js",
		"dist/assets/index.js",
	})
	if len(got) != 2 || got[0] != "src/ok.go" || got[1] != ".github/workflows/ci.yml" {
		t.Fatalf("%v", got)
	}
}

func TestDogfoodClaimHashFloodDoesNotDuplicate(t *testing.T) {
	st := dogfoodStore(t)
	text := "We decided to use jose, not jsonwebtoken, for Edge in src/middleware/auth.ts."
	var b strings.Builder
	for i := 0; i < 30; i++ {
		b.WriteString(grokLine("assistant", text))
	}
	dogfoodCatch(t, st, "acme/app", "dup", b.String())
	n := 0
	claims, _ := st.ListActive("acme/app")
	for _, c := range claims {
		if strings.Contains(c.Text, "jose") {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("expected 1 active jose claim, got %d", n)
	}
}
