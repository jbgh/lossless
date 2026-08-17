package eval

// Year-plus multi-discipline sim: generate Grok/Claude/Codex JSONL
// sessions across mobile, kernel, frontend, backend, game, infra,
// embedded, and data, ingest via CatchUp, backdate golds, then ask
// as if it is Aug 2026. Inspect + Explain dump tape/claims/why.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"lossless/internal/claim"
	"lossless/internal/inspect"
	"lossless/internal/retrieve"
	"lossless/internal/store"
	"lossless/internal/write"
)

var discNow = time.Date(2026, 8, 16, 15, 0, 0, 0, time.UTC)

type discGold struct {
	sess                          int // 0-based session of this world
	when, typ, text, path, needle string
}

type discWorld struct {
	name, project string
	gold          []discGold
	chatter       [][2]string // path, noun
}

func discWorlds() []discWorld {
	return []discWorld{
		{
			name: "mobile", project: "acme/lens",
			gold: []discGold{
				{0, "2025-08-22T18:00:00Z", "decision", "We decided to use a same-window hero overlay instead of a hard cut in ios/LightboxView.swift.", "ios/LightboxView.swift", "same-window hero"},
				{12, "2025-11-12T16:00:00Z", "failed", "Who-reacted failed in preview in ios/LightboxView.swift.", "ios/LightboxView.swift", "Who-reacted"},
				{26, "2026-02-18T12:00:00Z", "constraint", "Always never run mobile-down --full on the shared emulator in scripts/mobile/mobile-up.sh.", "scripts/mobile/mobile-up.sh", "mobile-down --full"},
			},
			chatter: [][2]string{
				{"ios/GridCell.swift", "prefetch window"},
				{"android/Lightbox.kt", "two-finger twist"},
				{"ios/WhoReactedView.swift", "reaction chip"},
				{"android/NavHost.kt", "hero overlay"},
			},
		},
		{
			name: "kernel", project: "acme/nic",
			gold: []discGold{
				{0, "2025-08-25T17:00:00Z", "decision", "We decided to use MSI-X, not legacy INTx, in drivers/net/acme_nic.c.", "drivers/net/acme_nic.c", "MSI-X"},
				{12, "2025-12-04T15:00:00Z", "failed", "DMA map failed in drivers/net/acme_nic.c on the IOMMU path.", "drivers/net/acme_nic.c", "DMA map"},
				{26, "2026-03-09T11:00:00Z", "constraint", "Never sleep in the hardirq handler in drivers/net/acme_nic.c.", "drivers/net/acme_nic.c", "hardirq"},
			},
			chatter: [][2]string{
				{"drivers/net/acme_ring.c", "rx ring"},
				{"include/linux/acme_nic.h", "doorbell"},
				{"drivers/net/acme_ethtool.c", "stats dump"},
				{"drivers/net/acme_reset.c", "flr path"},
			},
		},
		{
			name: "frontend", project: "acme/web",
			gold: []discGold{
				{0, "2025-09-03T18:00:00Z", "decision", "We decided to use CSS modules, not Tailwind, in src/app/shell.tsx.", "src/app/shell.tsx", "CSS modules"},
				{12, "2026-01-14T16:00:00Z", "failed", "Hydration mismatch failed in src/app/shell.tsx on the first paint.", "src/app/shell.tsx", "Hydration mismatch"},
				{26, "2026-04-02T12:00:00Z", "constraint", "Never ship a blocking webfont in src/app/shell.tsx.", "src/app/shell.tsx", "blocking webfont"},
			},
			chatter: [][2]string{
				{"src/app/Nav.tsx", "skip link"},
				{"src/styles/tokens.css", "density token"},
				{"src/app/Search.tsx", "combobox"},
				{"src/app/Footer.tsx", "locale switch"},
			},
		},
		{
			name: "backend", project: "acme/api",
			gold: []discGold{
				{0, "2025-08-20T18:00:00Z", "decision", "We decided to use jose, not jsonwebtoken, for Edge in src/middleware/auth.ts.", "src/middleware/auth.ts", "jose"},
				{12, "2025-11-03T16:00:00Z", "failed", "Redis token bucket failed in src/middleware/auth.ts staging.", "src/middleware/auth.ts", "Redis token bucket"},
				{26, "2026-02-13T12:00:00Z", "constraint", "Always never log Authorization headers in src/middleware/auth.ts.", "src/middleware/auth.ts", "Authorization"},
			},
			chatter: [][2]string{
				{"src/billing/export.ts", "warehouse cursor"},
				{"src/jobs/cron.ts", "cron lock"},
				{"src/db/client.ts", "pool size"},
				{"src/email/send.ts", "ses sandbox"},
			},
		},
		{
			name: "game", project: "acme/arena",
			gold: []discGold{
				{0, "2025-09-15T19:00:00Z", "decision", "We decided to use a deterministic 30Hz tick instead of Unity Update in src/sim/Tick.cs.", "src/sim/Tick.cs", "30Hz tick"},
				{12, "2026-01-08T17:00:00Z", "failed", "Physics rollback failed in src/sim/Tick.cs when two clients reconciled.", "src/sim/Tick.cs", "Physics rollback"},
				{26, "2026-05-20T13:00:00Z", "constraint", "Never mutate ECS state off the sim thread in src/sim/Tick.cs.", "src/sim/Tick.cs", "sim thread"},
			},
			chatter: [][2]string{
				{"src/net/Snapshot.cs", "delta compress"},
				{"src/render/Batch.cs", "sprite atlas"},
				{"src/ai/NavGrid.cs", "flow field"},
				{"src/ui/Hud.cs", "ammo counter"},
			},
		},
		{
			name: "infra", project: "acme/pipes",
			gold: []discGold{
				{0, "2025-10-01T18:00:00Z", "decision", "We decided to use Pulumi, not Terraform, in infra/stack.ts.", "infra/stack.ts", "Pulumi"},
				{12, "2026-02-27T15:00:00Z", "failed", "State lock failed in infra/stack.ts during the Friday apply.", "infra/stack.ts", "State lock"},
				{26, "2026-06-11T10:00:00Z", "constraint", "Never apply from a laptop to prod in infra/stack.ts.", "infra/stack.ts", "laptop to prod"},
			},
			chatter: [][2]string{
				{"infra/vpc.ts", "nat gateway"},
				{"infra/dns.ts", "alias record"},
				{"infra/ci.yml", "oidc role"},
				{"infra/secrets.ts", "kms wrap"},
			},
		},
		{
			name: "embedded", project: "acme/fw",
			gold: []discGold{
				{0, "2025-09-08T18:00:00Z", "decision", "We decided to use FreeRTOS, not bare metal, in src/rtos/main.c.", "src/rtos/main.c", "FreeRTOS"},
				{12, "2025-12-18T15:00:00Z", "failed", "Watchdog reset failed in src/rtos/main.c on brownout.", "src/rtos/main.c", "Watchdog reset"},
				{26, "2026-04-16T11:00:00Z", "constraint", "Never disable interrupts around I2C in src/rtos/main.c.", "src/rtos/main.c", "disable interrupts"},
			},
			chatter: [][2]string{
				{"src/hal/i2c.c", "nack retry"},
				{"src/hal/adc.c", "oversample"},
				{"src/boot/vector.c", "vtors"},
				{"src/net/lwip.c", "mtu clamp"},
			},
		},
		{
			name: "data", project: "acme/lake",
			gold: []discGold{
				{0, "2025-10-10T18:00:00Z", "decision", "We decided to use Iceberg, not Hive, in jobs/compact.py.", "jobs/compact.py", "Iceberg"},
				{12, "2026-01-22T16:00:00Z", "failed", "Compaction failed in jobs/compact.py on the late partition.", "jobs/compact.py", "Compaction failed"},
				{26, "2026-05-07T10:00:00Z", "constraint", "Never drop a partition from a laptop in jobs/compact.py.", "jobs/compact.py", "drop a partition"},
			},
			chatter: [][2]string{
				{"jobs/ingest.py", "schema drift"},
				{"jobs/dq.py", "null spike"},
				{"jobs/backfill.py", "watermark"},
				{"jobs/expire.py", "retain days"},
			},
		},
	}
}

func TestYearDisciplinesCatchUpAndAsk(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	dir := t.TempDir()
	worlds := discWorlds()
	start := time.Date(2025, 8, 18, 12, 0, 0, 0, time.UTC)
	sessions, extracted := 0, 0
	t0 := time.Now()

	sess := 0
	for d := 0; d < 365; d++ {
		day := start.AddDate(0, 0, d)
		if day.Weekday() == time.Saturday || day.Weekday() == time.Sunday {
			continue
		}
		w := worlds[sess%len(worlds)]
		worldSess := sess / len(worlds)
		body := discSession(w, sess, worldSess)
		sid := fmt.Sprintf("%s-d%03d", w.name, sess)
		p := filepath.Join(dir, sid+".jsonl")
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		res, err := write.CatchUp(st, write.CatchUpRequest{
			JSONL: p, Project: w.project, Harness: discHarness(sess),
			SessionID: sid, Source: "import",
		})
		if err != nil {
			t.Fatal(sid, err)
		}
		sessions++
		sess++
		extracted += res.Extracted
	}
	t.Logf("ingest sessions=%d extracted=%d in %s", sessions, extracted, time.Since(t0))
	if sessions < 240 {
		t.Fatalf("too few weekday sessions: %d", sessions)
	}
	if extracted < sessions {
		t.Fatalf("extract too thin: %d from %d", extracted, sessions)
	}

	if err := discBackdate(st, worlds); err != nil {
		t.Fatal(err)
	}
	for _, w := range worlds {
		recs, _ := st.ListActive(w.project)
		noise := 0
		var found []string
		for _, c := range recs {
			if retrieve.ExtractNoise(c) {
				noise++
			}
		}
		for _, g := range w.gold {
			ok := false
			for _, c := range recs {
				if strings.Contains(c.Text, g.needle) {
					ok = true
					found = append(found, g.needle+":"+c.Type+":"+c.CreatedAt)
				}
			}
			if !ok {
				t.Errorf("extract missed %s gold %q", w.name, g.needle)
			}
		}
		rep, err := inspect.Build(st, w.project)
		if err != nil {
			t.Fatal(w.name, err)
		}
		t.Logf("inspect %s  claims=%d noise=%d golds=%s", w.name, len(recs), noise, strings.Join(found, " | "))
		if rep.Detail == nil || rep.Detail.ByType["failed"] == 0 || rep.Detail.ByType["decision"] == 0 {
			t.Errorf("%s inspect types %+v", w.name, rep.Detail)
		}
	}

	eng := retrieve.Engine{Store: st, Now: func() time.Time { return discNow }}
	var hits, miss int
	for _, ask := range discAsks() {
		tr, err := eng.Explain(ask.req)
		if err != nil {
			t.Fatal(ask.name, err)
		}
		blob := discTraceBlob(tr)
		ok := true
		for _, n := range ask.needles {
			if !strings.Contains(blob, n) {
				t.Errorf("%s: missing %q packed=%s dropped=%s", ask.name, n, discPacked(tr), discDropped(tr))
				ok = false
			}
		}
		for _, n := range ask.forbid {
			if strings.Contains(blob, n) {
				t.Errorf("%s: leaked %q packed=%s", ask.name, n, discPacked(tr))
				ok = false
			}
		}
		if discNoisePacked(tr) {
			t.Errorf("%s: extract noise packed: %s", ask.name, discPacked(tr))
			ok = false
		}
		if ok {
			hits++
			t.Logf("OK  %-24s %s", ask.name, discPacked(tr))
		} else {
			miss++
			t.Logf("MISS %-24s dropped %s", ask.name, discDropped(tr))
		}
	}
	t.Logf("discipline asks %d/%d corpus=%d worlds=%d", hits, hits+miss, st.CountActive(), len(worlds))
	if miss > 0 {
		t.Fatalf("discipline year accuracy %d/%d", hits, hits+miss)
	}
}

type discAsk struct {
	name            string
	req             retrieve.Request
	needles, forbid []string
}

func discAsks() []discAsk {
	return []discAsk{
		{
			name:    "mobile-year-later-hero",
			req:     retrieve.Request{Project: "acme/lens", Question: "how does lightbox open", Paths: []string{"ios/LightboxView.swift"}},
			needles: []string{"same-window hero"},
			forbid:  []string{"MSI-X", "Pulumi", "jose", "30Hz", "FreeRTOS", "Iceberg"},
		},
		{
			name:    "mobile-who-reacted",
			req:     retrieve.Request{Project: "acme/lens", Goal: "fix who-reacted preview", Paths: []string{"ios/LightboxView.swift"}},
			needles: []string{"Who-reacted"},
			forbid:  []string{"DMA map", "Hydration"},
		},
		{
			name:    "mobile-emulator",
			req:     retrieve.Request{Project: "acme/lens", Question: "can I tear down the emulator"},
			needles: []string{"mobile-down --full"},
		},
		{
			name:    "kernel-msix",
			req:     retrieve.Request{Project: "acme/nic", Question: "which interrupt model", Paths: []string{"drivers/net/acme_nic.c"}},
			needles: []string{"MSI-X"},
			forbid:  []string{"same-window hero", "CSS modules", "jose", "FreeRTOS", "Iceberg"},
		},
		{
			name:    "kernel-dma",
			req:     retrieve.Request{Project: "acme/nic", Goal: "fix dma mapping", Paths: []string{"drivers/net/acme_nic.c"}},
			needles: []string{"DMA map"},
			forbid:  []string{"Who-reacted", "Redis token bucket"},
		},
		{
			name:    "kernel-irq-sleep",
			req:     retrieve.Request{Project: "acme/nic", Question: "can we sleep in the irq handler", Paths: []string{"drivers/net/acme_nic.c"}},
			needles: []string{"hardirq"},
		},
		{
			name:    "frontend-css",
			req:     retrieve.Request{Project: "acme/web", Question: "why not Tailwind", Paths: []string{"src/app/shell.tsx"}},
			needles: []string{"CSS modules"},
			forbid:  []string{"MSI-X", "30Hz", "Pulumi"},
		},
		{
			name:    "frontend-hydrate",
			req:     retrieve.Request{Project: "acme/web", Goal: "fix first paint", Paths: []string{"src/app/shell.tsx"}},
			needles: []string{"Hydration mismatch"},
			forbid:  []string{"Physics rollback"},
		},
		{
			name:    "frontend-font",
			req:     retrieve.Request{Project: "acme/web", Question: "webfont loading", Paths: []string{"src/app/shell.tsx"}},
			needles: []string{"blocking webfont"},
		},
		{
			name:    "backend-jose-year-later",
			req:     retrieve.Request{Project: "acme/api", Question: "why not jsonwebtoken", Goal: "pick a JWT library", Paths: []string{"src/middleware/auth.ts"}},
			needles: []string{"jose"},
			forbid:  []string{"Warehouse", "MSI-X", "Pulumi"},
		},
		{
			name:    "backend-redis",
			req:     retrieve.Request{Project: "acme/api", Goal: "add rate limiting", Paths: []string{"src/middleware/auth.ts"}},
			needles: []string{"Redis token bucket"},
			forbid:  []string{"State lock", "DMA map"},
		},
		{
			name:    "backend-headers",
			req:     retrieve.Request{Project: "acme/api", Question: "Authorization headers", Paths: []string{"src/middleware/auth.ts"}},
			needles: []string{"Authorization"},
		},
		{
			name:    "game-tick",
			req:     retrieve.Request{Project: "acme/arena", Question: "why not Unity Update", Paths: []string{"src/sim/Tick.cs"}},
			needles: []string{"30Hz tick"},
			forbid:  []string{"jose", "CSS modules", "Pulumi"},
		},
		{
			name:    "game-rollback",
			req:     retrieve.Request{Project: "acme/arena", Goal: "fix reconcile", Paths: []string{"src/sim/Tick.cs"}},
			needles: []string{"Physics rollback"},
			forbid:  []string{"Hydration mismatch"},
		},
		{
			name:    "game-ecs-thread",
			req:     retrieve.Request{Project: "acme/arena", Question: "can render write ECS", Paths: []string{"src/sim/Tick.cs"}},
			needles: []string{"sim thread"},
		},
		{
			name:    "infra-pulumi",
			req:     retrieve.Request{Project: "acme/pipes", Question: "why not Terraform", Paths: []string{"infra/stack.ts"}},
			needles: []string{"Pulumi"},
			forbid:  []string{"jose", "MSI-X", "same-window hero"},
		},
		{
			name:    "infra-lock",
			req:     retrieve.Request{Project: "acme/pipes", Goal: "fix apply", Paths: []string{"infra/stack.ts"}},
			needles: []string{"State lock"},
			forbid:  []string{"Redis token bucket"},
		},
		{
			name:    "infra-laptop",
			req:     retrieve.Request{Project: "acme/pipes", Question: "apply from my laptop", Paths: []string{"infra/stack.ts"}},
			needles: []string{"laptop to prod"},
		},
		{
			name:    "embedded-rtos",
			req:     retrieve.Request{Project: "acme/fw", Question: "why not bare metal", Paths: []string{"src/rtos/main.c"}},
			needles: []string{"FreeRTOS"},
			forbid:  []string{"Iceberg", "jose", "Pulumi", "same-window hero"},
		},
		{
			name:    "embedded-watchdog",
			req:     retrieve.Request{Project: "acme/fw", Goal: "fix brownout reset", Paths: []string{"src/rtos/main.c"}},
			needles: []string{"Watchdog reset"},
			forbid:  []string{"Compaction failed", "DMA map"},
		},
		{
			name:    "embedded-i2c-irq",
			req:     retrieve.Request{Project: "acme/fw", Question: "can we disable irq around i2c", Paths: []string{"src/rtos/main.c"}},
			needles: []string{"disable interrupts"},
		},
		{
			name:    "data-iceberg",
			req:     retrieve.Request{Project: "acme/lake", Question: "why not Hive", Paths: []string{"jobs/compact.py"}},
			needles: []string{"Iceberg"},
			forbid:  []string{"FreeRTOS", "jose", "30Hz"},
		},
		{
			name:    "data-compact",
			req:     retrieve.Request{Project: "acme/lake", Goal: "fix late partition", Paths: []string{"jobs/compact.py"}},
			needles: []string{"Compaction failed"},
			forbid:  []string{"Watchdog reset", "State lock"},
		},
		{
			name:    "data-drop-partition",
			req:     retrieve.Request{Project: "acme/lake", Question: "drop partition from my laptop", Paths: []string{"jobs/compact.py"}},
			needles: []string{"drop a partition"},
		},
		{
			name:    "empty-project",
			req:     retrieve.Request{Project: "acme/none", Question: "jose"},
			needles: nil,
			forbid:  []string{"jose", "Pulumi", "Who-reacted", "FreeRTOS", "Iceberg"},
		},
	}
}

func discTraceBlob(tr retrieve.Trace) string {
	var b strings.Builder
	for _, h := range tr.Packed {
		b.WriteString(h.Type)
		b.WriteByte(':')
		b.WriteString(h.Text)
		b.WriteByte('\n')
	}
	return b.String()
}

func discPacked(tr retrieve.Trace) string {
	var parts []string
	for _, h := range tr.Packed {
		parts = append(parts, h.Type+":"+clipDisc(h.Text, 42)+" ["+h.Why+"]")
	}
	if len(parts) == 0 {
		return "(empty)"
	}
	return strings.Join(parts, " | ")
}

func discDropped(tr retrieve.Trace) string {
	var parts []string
	for i, h := range tr.Dropped {
		if i >= 4 {
			break
		}
		parts = append(parts, h.Type+":"+clipDisc(h.Text, 36)+" ["+h.Why+"]")
	}
	if len(parts) == 0 {
		return "(none)"
	}
	return strings.Join(parts, " | ")
}

func discNoisePacked(tr retrieve.Trace) bool {
	for _, h := range tr.Packed {
		if retrieve.ExtractNoise(claim.Record{Type: h.Type, Text: h.Text, Paths: h.Paths}) {
			return true
		}
	}
	return false
}

func clipDisc(s string, n int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func discSession(w discWorld, sess, worldSess int) string {
	ch := w.chatter[sess%len(w.chatter)]
	path, noun := ch[0], ch[1]
	h := discHarness(sess)
	var b strings.Builder
	switch h {
	case "claude":
		b.WriteString(claudeLine("user", fmt.Sprintf("Look at %s today.", path)))
		b.WriteString(claudeLine("assistant", "I'll check what we already decided, then inspect the file."))
		b.WriteString(claudeLine("assistant", fmt.Sprintf("Checking #%d failure and re-pushing both.", 3000+sess)))
		b.WriteString(claudeLine("assistant", fmt.Sprintf("The %s failed in %s on weekday %d.", noun, path, sess)))
		b.WriteString(claudeLine("assistant", fmt.Sprintf("We decided to keep the %s in %s instead of rewriting it.", noun, path)))
	case "codex":
		b.WriteString(codexUser(fmt.Sprintf("Look at %s today.", path)))
		b.WriteString(codexAgent("I'll start with the current plan instead of jumping in."))
		b.WriteString(codexAgent(fmt.Sprintf("The %s failed in %s on weekday %d.", noun, path, sess)))
		b.WriteString(codexAgent(fmt.Sprintf("We decided to keep the %s in %s.", noun, path)))
	default:
		b.WriteString(grokLine("user", fmt.Sprintf("Look at %s today.", path)))
		b.WriteString(grokLine("assistant", "I'll look at retrieve extract and then keep going."))
		b.WriteString(grokLine("assistant", fmt.Sprintf("## Investigation: why the %s failed", noun)))
		b.WriteString(grokLine("assistant", fmt.Sprintf("The %s failed in %s on weekday %d.", noun, path, sess)))
		b.WriteString(grokLine("assistant", fmt.Sprintf("We decided to keep the %s in %s.", noun, path)))
	}

	for _, g := range w.gold {
		if g.sess == worldSess {
			b.WriteString(discGoldLine(h, g))
		}
	}
	if sess%11 == 0 {
		b.WriteString(grokLine("assistant", `{"context":[{"id":"x","type":"failed","text":"Redis token bucket failed in src/middleware/auth.ts staging."}],"warnings":["A prior attempt failed"],"tokens":8,"project":"acme/api"}`))
	}
	if sess%13 == 0 {
		b.WriteString(grokLine("user", "Look at "+path+".\n<system-reminder>\nAlways never use the simulator. Don't change source.\n</system-reminder>"))
	}
	return b.String()
}

func discGoldLine(harness string, g discGold) string {
	switch g.typ {
	case "constraint":
		switch harness {
		case "claude":
			return claudeLine("user", g.text)
		case "codex":
			return codexUser(g.text)
		default:
			return grokLine("user", g.text)
		}
	default:
		switch harness {
		case "claude":
			return claudeLine("assistant", g.text)
		case "codex":
			return codexAgent(g.text)
		default:
			return grokLine("assistant", g.text)
		}
	}
}

func discHarness(d int) string {
	switch d % 3 {
	case 0:
		return "claude"
	case 1:
		return "codex"
	default:
		return "grok"
	}
}

func claudeLine(role, text string) string {
	return fmt.Sprintf(`{"type":%q,"message":{"role":%q,"content":[{"type":"text","text":%q}]}}`+"\n", role, role, text)
}

func codexUser(text string) string {
	return fmt.Sprintf(`{"type":"event_msg","payload":{"type":"user_message","message":%q}}`+"\n", text)
}

func codexAgent(text string) string {
	return fmt.Sprintf(`{"type":"event_msg","payload":{"type":"agent_message","message":%q}}`+"\n", text)
}

func discBackdate(st *store.Store, worlds []discWorld) error {
	for _, w := range worlds {
		for _, g := range w.gold {
			if _, err := st.DB.Exec(
				`UPDATE records SET created_at = ? WHERE status = 'active' AND project_key = ? AND text LIKE ?`,
				g.when, w.project, "%"+g.needle+"%",
			); err != nil {
				return err
			}
		}
	}
	return nil
}

func liveNoiseHits(out retrieve.Response) bool {
	for _, h := range out.Context {
		if liveNoise(h) != "" {
			return true
		}
	}
	return false
}
