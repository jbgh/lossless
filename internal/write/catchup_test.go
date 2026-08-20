package write

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"lossless/internal/claim"
	"lossless/internal/store"
)

func tmpStore(t *testing.T) *store.Store {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

func writeJSONL(t *testing.T, dir, name, body string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestCatchUpCopiesIntoOwnedRaw(t *testing.T) {
	st := tmpStore(t)
	src := writeJSONL(t, t.TempDir(), "chat.jsonl",
		`{"type":"assistant","content":"We decided to use jose, not jsonwebtoken, for Edge."}`+"\n")
	res, err := CatchUp(st, CatchUpRequest{JSONL: src, Project: "acme/api", Harness: "grok", SessionID: "s1"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Copied == 0 {
		t.Fatal("expected bytes copied")
	}
	raw, err := os.ReadFile(res.RawPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "jose") {
		t.Fatalf("raw missing content: %s", raw)
	}
	if !strings.Contains(res.RawPath, "raw/") {
		t.Fatalf("raw not under raw/: %s", res.RawPath)
	}
}

func TestCatchUpRefusesLiveTestIngest(t *testing.T) {
	root := filepath.Join(os.TempDir(), "ll-refuse-live")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	st, err := store.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	ws := t.TempDir()
	src := writeJSONL(t, ws, "chat.jsonl",
		`{"type":"assistant","content":"We decided to use jose, not jsonwebtoken, for Edge."}`+"\n")
	res, err := CatchUp(st, CatchUpRequest{
		JSONL: src, WorkspaceRoot: ws, Harness: "grok", SessionID: "sess1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Noop || res.Extracted != 0 {
		t.Fatalf("live home ingested a go-test session: %+v", res)
	}
	if st.CountActive() != 0 {
		t.Fatal("claims leaked")
	}
}

func TestGoTestPath(t *testing.T) {
	if !goTestPath("/var/folders/xx/T/TestRunHookGrok805684300/003") {
		t.Fatal("go test temp")
	}
	if goTestPath("/Users/jay/developer/lossless") || goTestPath("/tmp/TestingApp") {
		t.Fatal("real path")
	}
}

func TestCatchUpIdempotent(t *testing.T) {
	st := tmpStore(t)
	src := writeJSONL(t, t.TempDir(), "chat.jsonl",
		`{"type":"assistant","content":"We decided to use jose over jsonwebtoken."}`+"\n")
	req := CatchUpRequest{JSONL: src, Project: "acme/api", SessionID: "s1"}
	a, err := CatchUp(st, req)
	if err != nil {
		t.Fatal(err)
	}
	b, err := CatchUp(st, req)
	if err != nil {
		t.Fatal(err)
	}
	if !b.Noop {
		t.Fatalf("second catch-up should no-op: %+v", b)
	}
	info, _ := os.Stat(a.RawPath)
	if info.Size() == 0 {
		t.Fatal("raw empty")
	}
}

func TestCatchUpTwoTurns(t *testing.T) {
	st := tmpStore(t)
	p := filepath.Join(t.TempDir(), "chat.jsonl")
	if err := os.WriteFile(p, []byte(`{"type":"user","content":"Always use jose."}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := CatchUp(st, CatchUpRequest{JSONL: p, Project: "acme/api", SessionID: "s1"}); err != nil {
		t.Fatal(err)
	}
	f, _ := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0o644)
	_, _ = f.WriteString(`{"type":"assistant","content":"Redis token bucket failed in staging."}` + "\n")
	_ = f.Close()
	res, err := CatchUp(st, CatchUpRequest{JSONL: p, Project: "acme/api", SessionID: "s1"})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(res.RawPath)
	if !strings.Contains(string(raw), "jose") || !strings.Contains(string(raw), "Redis") {
		t.Fatalf("raw missing a turn: %s", raw)
	}
}

func TestCatchUpResetsWhenHarnessFileShrinks(t *testing.T) {
	st := tmpStore(t)
	p := filepath.Join(t.TempDir(), "chat.jsonl")
	old := `{"type":"user","content":"Always use jose."}` + "\n" +
		`{"type":"assistant","content":"ok we will use jose for Edge."}` + "\n" +
		`{"type":"user","content":"also never log Authorization headers."}` + "\n"
	if err := os.WriteFile(p, []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}
	first, err := CatchUp(st, CatchUpRequest{JSONL: p, Project: "acme/api", SessionID: "s-shrink"})
	if err != nil || first.Copied == 0 {
		t.Fatalf("first %+v %v", first, err)
	}
	rewritten := `{"type":"user","content":"continue after compact"}` + "\n" +
		`{"type":"assistant","content":"We decided to keep the limiter in-process."}` + "\n"
	if err := os.WriteFile(p, []byte(rewritten), 0o644); err != nil {
		t.Fatal(err)
	}
	second, err := CatchUp(st, CatchUpRequest{JSONL: p, Project: "acme/api", SessionID: "s-shrink", Source: "compact"})
	if err != nil {
		t.Fatal(err)
	}
	if second.Noop || second.Copied == 0 {
		t.Fatalf("shrink must recopy, got %+v", second)
	}
	raw, err := os.ReadFile(second.RawPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "continue after compact") {
		t.Fatalf("missing rewritten tail: %s", raw)
	}
	if st.Cursor(p) != int64(len(rewritten)) {
		t.Fatalf("cursor %d want %d", st.Cursor(p), len(rewritten))
	}
}

func TestCatchUpSurvivesHarnessDelete(t *testing.T) {
	st := tmpStore(t)
	src := writeJSONL(t, t.TempDir(), "chat.jsonl",
		`{"type":"assistant","content":"We decided to use jose, not jsonwebtoken, for Edge."}`+"\n")
	res, err := CatchUp(st, CatchUpRequest{JSONL: src, Project: "acme/api", SessionID: "s1"})
	if err != nil {
		t.Fatal(err)
	}
	_ = os.Remove(src)
	raw, err := os.ReadFile(res.RawPath)
	if err != nil || !strings.Contains(string(raw), "jose") {
		t.Fatal("owned raw should survive harness delete")
	}
	claims, err := st.ListActive("acme/api")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, c := range claims {
		if strings.Contains(c.Text, "jose") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected jose claim, got %+v", claims)
	}
}

func TestCatchUpRedactsSecrets(t *testing.T) {
	st := tmpStore(t)
	src := writeJSONL(t, t.TempDir(), "chat.jsonl",
		`{"type":"assistant","content":"oops the key is AKIAIOSFODNN7EXAMPLE do not commit"}`+"\n")
	res, err := CatchUp(st, CatchUpRequest{JSONL: src, Project: "acme/api", SessionID: "s1"})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(res.RawPath)
	if strings.Contains(string(raw), "AKIA") {
		t.Fatalf("secret leaked into raw: %s", raw)
	}
	if !strings.Contains(string(raw), `_redacted`) {
		t.Fatalf("expected redacted marker: %s", raw)
	}
	claims, _ := st.ListActive("acme/api")
	for _, c := range claims {
		if strings.Contains(c.Text, "AKIA") {
			t.Fatal("secret in claim")
		}
	}
}

func TestRememberWritesClaimAndRaw(t *testing.T) {
	st := tmpStore(t)
	res, err := Remember(st, claim.Record{
		Type: "decision", Text: "Use jose, not jsonwebtoken, for Edge.", ProjectKey: "acme/api",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Extracted != 1 {
		t.Fatalf("extracted=%d", res.Extracted)
	}
	raw, err := os.ReadFile(res.RawPath)
	if err != nil || !strings.Contains(string(raw), "jose") {
		t.Fatal("remember raw missing")
	}
	got, ok := st.Get(res.IDs[0])
	if !ok || !strings.Contains(got.Text, "jose") {
		t.Fatal("claim missing")
	}
	if got.TranscriptRef == nil || got.TranscriptRef.SessionID != "manual" {
		t.Fatalf("ref %+v", got.TranscriptRef)
	}
	view, ok := st.View(res.IDs[0])
	if !ok || !strings.Contains(view.Excerpt, "jose") {
		t.Fatalf("excerpt %q", view.Excerpt)
	}
}

func TestRememberSupersedes(t *testing.T) {
	st := tmpStore(t)
	a, err := Remember(st, claim.Record{Type: "decision", Text: "Use jose, not jsonwebtoken, for Edge.", ProjectKey: "acme/api"})
	if err != nil {
		t.Fatal(err)
	}
	b, err := Remember(st, claim.Record{Type: "decision", Text: "Use jose, not jsonwebtoken, for Edge.", ProjectKey: "acme/api"})
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Superseded) != 1 || b.Superseded[0] != a.IDs[0] {
		t.Fatalf("expected supersede of %s, got %+v", a.IDs, b)
	}
	old, ok := st.Get(a.IDs[0])
	if !ok || old.Status != "superseded" {
		t.Fatalf("old status=%s", old.Status)
	}
}

func TestCatchUpValidationAndDefaults(t *testing.T) {
	st := tmpStore(t)
	if _, err := CatchUp(st, CatchUpRequest{}); err == nil {
		t.Fatal("jsonl required")
	}
	src := writeJSONL(t, t.TempDir(), "chat.jsonl",
		`{"type":"assistant","content":"We decided to use jose, not jsonwebtoken, for Edge."}`+"\n")
	if _, err := CatchUp(st, CatchUpRequest{JSONL: src}); err == nil {
		t.Fatal("project required")
	}
	if _, err := CatchUp(st, CatchUpRequest{JSONL: filepath.Join(t.TempDir(), "missing.jsonl"), Project: "acme/api"}); err == nil {
		t.Fatal("stat")
	}
	res, err := CatchUp(st, CatchUpRequest{JSONL: src, Project: "Acme/API"})
	if err != nil || res.Extracted == 0 {
		t.Fatal(res, err)
	}
	// wait for a full line
	partial := writeJSONL(t, t.TempDir(), "p.jsonl", `{"type":"user","content":"no nl yet"}`)
	res, err = CatchUp(st, CatchUpRequest{JSONL: partial, Project: "acme/api", SessionID: "p"})
	if err != nil || res.Copied != 0 {
		t.Fatalf("partial: %+v %v", res, err)
	}
}

func TestCatchUpFromWorkspaceAndPartialTail(t *testing.T) {
	st := tmpStore(t)
	ws := t.TempDir()
	src := writeJSONL(t, t.TempDir(), "s.jsonl",
		`{"type":"assistant","content":"We decided to use jose, not jsonwebtoken, for Edge."}`+"\n"+
			`{"type":"user","content":"incomplete`)
	res, err := CatchUp(st, CatchUpRequest{JSONL: src, WorkspaceRoot: ws, SessionID: ""})
	if err != nil {
		t.Fatal(err)
	}
	if res.Copied == 0 {
		t.Fatal("expected complete first line copied")
	}
	if !strings.Contains(res.RawPath, "raw/") {
		t.Fatal(res.RawPath)
	}
}

func TestRememberRejectsUnsafeID(t *testing.T) {
	st := tmpStore(t)
	if _, err := Remember(st, claim.Record{
		ID: "../etc/passwd", Type: "decision",
		Text: "Use jose, not jsonwebtoken, for Edge.", ProjectKey: "acme/api",
	}); err == nil {
		t.Fatal("expected reject")
	}
	if _, err := os.Stat(filepath.Join(st.Root, "export", "etc", "passwd.md")); !os.IsNotExist(err) {
		t.Fatalf("escaped store: %v", err)
	}
}

func TestRememberDropsTraversalPaths(t *testing.T) {
	st := tmpStore(t)
	res, err := Remember(st, claim.Record{
		Type: "decision", Text: "Use jose, not jsonwebtoken, for Edge.",
		ProjectKey: "acme/api",
		Paths:      []string{"src/auth.ts", "../.ssh/id_rsa", "/etc/passwd", "src/ok.go"},
	})
	if err != nil {
		t.Fatal(err)
	}
	got, ok := st.Get(res.IDs[0])
	if !ok {
		t.Fatal("missing")
	}
	if len(got.Paths) != 2 || got.Paths[0] != "src/auth.ts" || got.Paths[1] != "src/ok.go" {
		t.Fatalf("paths: %v", got.Paths)
	}
}

func TestRememberValidationAndDefaults(t *testing.T) {
	st := tmpStore(t)
	if _, err := Remember(st, claim.Record{}); err == nil {
		t.Fatal("type/text")
	}
	if _, err := Remember(st, claim.Record{Type: "decision", Text: "Use jose, not jsonwebtoken, for Edge."}); err == nil {
		t.Fatal("project")
	}
	ws := t.TempDir()
	res, err := Remember(st, claim.Record{
		Type: "decision", Text: "Use jose, not jsonwebtoken, for Edge.", WorkspaceRoot: ws,
	})
	if err != nil || res.Extracted != 1 {
		t.Fatal(res, err)
	}
	got, ok := st.Get(res.IDs[0])
	if !ok || got.Harness != "other" || got.Source != "remember" || got.SessionID != "manual" {
		t.Fatalf("%+v", got)
	}
}

func TestCatchUpSessionFromFilename(t *testing.T) {
	st := tmpStore(t)
	src := writeJSONL(t, t.TempDir(), "fromname.jsonl",
		`{"type":"assistant","content":"We decided to use jose, not jsonwebtoken, for Edge."}`+"\n")
	res, err := CatchUp(st, CatchUpRequest{JSONL: src, Project: "acme/api", Harness: "", Source: ""})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.RawPath, "fromname") {
		t.Fatal(res.RawPath)
	}
}

func TestCatchUpSkipsOwnAskPacketClaims(t *testing.T) {
	st := tmpStore(t)
	src := writeJSONL(t, t.TempDir(), "chat.jsonl",
		`{"type":"assistant","tool_calls":[{"id":"c1","name":"ask","arguments":"{}"}]}`+"\n"+
			`{"type":"tool_result","tool_call_id":"c1","content":"{\"context\":[{\"text\":\"Redis token bucket failed in staging; do not repeat.\"}],\"warnings\":[\"A prior attempt at this goal failed (see 01J).\"],\"tokens\":20,\"project\":\"acme/api\"}"}`+"\n"+
			`{"type":"assistant","content":"We decided to use jose, not jsonwebtoken, for Edge."}`+"\n")
	res, err := CatchUp(st, CatchUpRequest{JSONL: src, Project: "acme/api", SessionID: "s-ask"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Copied == 0 {
		t.Fatal("raw should still copy")
	}
	claims, err := st.ListActive("acme/api")
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range claims {
		if strings.Contains(c.Text, "Redis token bucket") || strings.Contains(c.Text, "prior attempt") {
			t.Fatalf("ask packet became a claim: %+v", c)
		}
	}
	found := false
	for _, c := range claims {
		if strings.Contains(c.Text, "jose") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected jose claim, got %+v", claims)
	}
	if _, ok := st.SessionByJSONL(src); !ok {
		t.Fatal("session not registered")
	}
}

func TestRememberBlockedRawDir(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	_ = os.RemoveAll(filepath.Join(dir, "raw"))
	if err := os.WriteFile(filepath.Join(dir, "raw"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Remember(st, claim.Record{Type: "decision", Text: "Use jose, not jsonwebtoken, for Edge.", ProjectKey: "acme/api"}); err == nil {
		t.Fatal("expected mkdir fail")
	}
}
