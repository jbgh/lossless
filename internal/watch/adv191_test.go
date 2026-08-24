package watch

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"lossless/internal/harness"
	"lossless/internal/projectkey"
	"lossless/internal/store"
)

// Adv191: 0.1.19 nested Claude — subagent dumps skip; cwd-nested non-subagent still catch-up.
func TestAdv191ClaudeSubagentDumpSkipped(t *testing.T) {
	root := t.TempDir()
	ws := "/Users/jay/dev/api"
	cdir := filepath.Join(root, "claude", harness.ClaudeProjectSlug(ws))
	if err := os.MkdirAll(cdir, 0o755); err != nil {
		t.Fatal(err)
	}
	parent := filepath.Join(cdir, "uuid-parent.jsonl")
	line := `{"type":"user","cwd":"` + ws + `","message":{"role":"user","content":"go"}}` + "\n" +
		`{"type":"assistant","content":"We decided to use jose, not jsonwebtoken, for Edge."}` + "\n"
	if err := os.WriteFile(parent, []byte(line), 0o644); err != nil {
		t.Fatal(err)
	}
	nestedDir := filepath.Join(cdir, "agent")
	if err := os.MkdirAll(nestedDir, 0o755); err != nil {
		t.Fatal(err)
	}
	nestedOK := filepath.Join(nestedDir, "nested-ok.jsonl")
	if err := os.WriteFile(nestedOK, []byte(line), 0o644); err != nil {
		t.Fatal(err)
	}
	subDir := filepath.Join(cdir, "uuid-parent", "subagents", "workflows", "wf_dead")
	if err := os.MkdirAll(subDir, 0o755); err != nil {
		t.Fatal(err)
	}
	dump := filepath.Join(subDir, "agent-deadbeef.jsonl")
	dumpBody := `{"type":"user","cwd":"` + ws + `","message":{"role":"user","content":"go"}}` + "\n" +
		`{"type":"assistant","content":"READ-ONLY: do not push, edit, or merge."}` + "\n"
	if err := os.WriteFile(dump, []byte(dumpBody), 0o644); err != nil {
		t.Fatal(err)
	}
	gdir := filepath.Join(root, "grok", harness.EncodeCWD(ws), "child-sid")
	if err := os.MkdirAll(gdir, 0o755); err != nil {
		t.Fatal(err)
	}
	gfile := filepath.Join(gdir, "chat_history.jsonl")
	if err := os.WriteFile(gfile, []byte(`{"type":"assistant","content":"We decided to use jose, not jsonwebtoken, for Edge."}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	opts := Options{GrokRoot: filepath.Join(root, "grok"), ClaudeRoot: filepath.Join(root, "claude")}
	found := Discover(opts, nil)
	var grok, claude, dumps int
	for _, f := range found {
		switch f.Harness {
		case "grok":
			grok++
		case "claude":
			claude++
			if strings.Contains(f.JSONL, "subagents") || strings.HasPrefix(filepath.Base(f.JSONL), "agent-") {
				dumps++
			}
		}
	}
	if grok != 1 || claude != 2 || dumps != 0 {
		t.Fatalf("discover grok=%d claude=%d dumps=%d %+v", grok, claude, dumps, found)
	}

	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	res, err := Tick(st, opts)
	if err != nil {
		t.Fatal(err)
	}
	if res.CatchUps < 3 {
		t.Fatalf("catch_ups=%d seen=%d", res.CatchUps, res.Seen)
	}
	project := projectkey.FromWorkspace(ws)
	recs, err := st.ListActive(project)
	if err != nil {
		t.Fatal(err)
	}
	blob := ""
	for _, r := range recs {
		blob += r.Text + "\n"
	}
	if strings.Contains(blob, "READ-ONLY") {
		t.Fatalf("subagent dump ingested: %s", blob)
	}
	if !strings.Contains(blob, "jose") {
		t.Fatalf("product nested/parent missed: %s", blob)
	}
}

func writeClaudeJSONL(t *testing.T, path, cwd, assistant string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	var body string
	if cwd == "" {
		body = `{"type":"user","message":{"role":"user","content":"go"}}` + "\n"
	} else {
		body = `{"type":"user","cwd":"` + cwd + `","message":{"role":"user","content":"go"}}` + "\n"
	}
	body += `{"type":"assistant","content":"` + assistant + `"}` + "\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func discoverHas(found []Target, path string) bool {
	for _, f := range found {
		if f.JSONL == path {
			return true
		}
	}
	return false
}

// Adv191 shapes the first test can miss: direct subagents/agent-*.jsonl,
// workflows dump, Agent-Foo case, unknown-cwd nested, grok child, nested-ok.
func TestAdv191DumpPathShapesDiscoverAndTick(t *testing.T) {
	root := t.TempDir()
	ws := "/Users/jay/dev/api"
	cdir := filepath.Join(root, "claude", harness.ClaudeProjectSlug(ws))
	parent := filepath.Join(cdir, "uuid-parent.jsonl")
	writeClaudeJSONL(t, parent, ws, "We decided to use parent-jose, not jsonwebtoken, for Edge.")

	nestedOK := filepath.Join(cdir, "agent", "nested-ok.jsonl")
	writeClaudeJSONL(t, nestedOK, ws, "We decided to use nested-ok-jose, not jsonwebtoken, for Edge.")

	unknownNested := filepath.Join(cdir, "agent", "nested-unknown.jsonl")
	writeClaudeJSONL(t, unknownNested, "", "We decided to use unknown-cwd-poison-jose, not jsonwebtoken, for Edge.")

	directDump := filepath.Join(cdir, "uuid-parent", "subagents", "agent-deadbeef.jsonl")
	writeClaudeJSONL(t, directDump, ws, "READ-ONLY: do not push, edit, or merge. We decided to use subagent-poison-jose, not jsonwebtoken, for Edge.")

	wfDump := filepath.Join(cdir, "uuid-parent", "subagents", "workflows", "wf_dead", "agent-wf.jsonl")
	writeClaudeJSONL(t, wfDump, ws, "READ-ONLY: do not push, edit, or merge. We decided to use workflow-poison-jose, not jsonwebtoken, for Edge.")

	caseDump := filepath.Join(cdir, "uuid-parent", "subagents", "Agent-Foo.jsonl")
	writeClaudeJSONL(t, caseDump, ws, "READ-ONLY: do not push, edit, or merge. We decided to use agentfoo-poison-jose, not jsonwebtoken, for Edge.")

	gdir := filepath.Join(root, "grok", harness.EncodeCWD(ws), "child-sid")
	if err := os.MkdirAll(gdir, 0o755); err != nil {
		t.Fatal(err)
	}
	gfile := filepath.Join(gdir, "chat_history.jsonl")
	if err := os.WriteFile(gfile, []byte(`{"type":"assistant","content":"We decided to use grok-child-jose, not jsonwebtoken, for Edge."}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	opts := Options{GrokRoot: filepath.Join(root, "grok"), ClaudeRoot: filepath.Join(root, "claude")}
	found := Discover(opts, nil)
	must := []string{parent, nestedOK, gfile}
	mustNot := []string{directDump, wfDump, caseDump, unknownNested}
	for _, p := range must {
		if !discoverHas(found, p) {
			t.Fatalf("Discover missed %s in %+v", p, found)
		}
	}
	for _, p := range mustNot {
		if discoverHas(found, p) {
			t.Fatalf("Discover included dump/unknown %s in %+v", p, found)
		}
	}
	var grok, claude int
	for _, f := range found {
		switch f.Harness {
		case "grok":
			grok++
			if f.JSONL != gfile {
				t.Fatalf("unexpected grok %s", f.JSONL)
			}
		case "claude":
			claude++
			if strings.Contains(f.JSONL, string(filepath.Separator)+"subagents"+string(filepath.Separator)) {
				t.Fatalf("dump leaked %s", f.JSONL)
			}
			base := strings.ToLower(filepath.Base(f.JSONL))
			if strings.HasPrefix(base, "agent-") && strings.HasSuffix(base, ".jsonl") {
				t.Fatalf("agent- dump leaked %s", f.JSONL)
			}
		}
	}
	if grok != 1 || claude != 2 {
		t.Fatalf("discover grok=%d claude=%d %+v", grok, claude, found)
	}

	t.Run("discover-known-dump", func(t *testing.T) {
		knownDump := []store.Session{{JSONL: directDump, Harness: "claude", SessionID: "agent-deadbeef", Workspace: ws}}
		if discoverHas(Discover(opts, knownDump), directDump) {
			t.Errorf("Discover(known) still surfaced subagents dump %s", directDump)
		}
	})

	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	res, err := Tick(st, opts)
	if err != nil {
		t.Fatal(err)
	}
	if res.CatchUps != 3 {
		t.Errorf("catch_ups=%d seen=%d want 3", res.CatchUps, res.Seen)
	}
	project := projectkey.FromWorkspace(ws)
	recs, err := st.ListActive(project)
	if err != nil {
		t.Fatal(err)
	}
	blob := ""
	for _, r := range recs {
		blob += r.Text + "\n"
	}
	for _, tok := range []string{"READ-ONLY", "subagent-poison-jose", "workflow-poison-jose", "agentfoo-poison-jose", "unknown-cwd-poison-jose"} {
		if strings.Contains(blob, tok) {
			t.Errorf("dump/unknown ingested %q: %s", tok, blob)
		}
	}
	for _, tok := range []string{"parent-jose", "nested-ok-jose", "grok-child-jose"} {
		if !strings.Contains(blob, tok) {
			t.Errorf("product session missed %q: %s", tok, blob)
		}
	}

	t.Run("tick-known-dump", func(t *testing.T) {
		if err := st.UpsertSession(store.Session{JSONL: directDump, Harness: "claude", SessionID: "agent-deadbeef", Workspace: ws}); err != nil {
			t.Fatal(err)
		}
		again, err := Tick(st, opts)
		if err != nil {
			t.Fatal(err)
		}
		recs, err := st.ListActive(project)
		if err != nil {
			t.Fatal(err)
		}
		blob := ""
		for _, r := range recs {
			blob += r.Text + "\n"
		}
		if strings.Contains(blob, "READ-ONLY") || strings.Contains(blob, "subagent-poison-jose") {
			t.Errorf("Tick extracted dump after known session: catch_ups=%d %s", again.CatchUps, blob)
		}
	})
}
