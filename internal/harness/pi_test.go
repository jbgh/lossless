package harness

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPiSessionSlugAndID(t *testing.T) {
	if PiSessionSlug("/Users/jay/dev/api") != "--Users-jay-dev-api--" {
		t.Fatal(PiSessionSlug("/Users/jay/dev/api"))
	}
	if PiSessionIDFromPath("/x/--Users-jay--/2024-12-03T14-00-00_abcdef12-3456.jsonl") != "abcdef12-3456" {
		t.Fatal(PiSessionIDFromPath("2024-12-03T14-00-00_abcdef12-3456.jsonl"))
	}
}

func TestLocatePi(t *testing.T) {
	root := t.TempDir()
	t.Setenv("PI_HOME", root)
	t.Setenv("HOME", root)
	ws := "/Users/jay/dev/api"
	sid := "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	dir := filepath.Join(root, "agent", "sessions", PiSessionSlug(ws))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, "2024-12-03T14-00-00_"+sid+".jsonl")
	body := `{"type":"session","version":3,"id":"` + sid + `","cwd":"` + ws + `"}` + "\n" +
		`{"type":"message","id":"a1","parentId":null,"message":{"role":"user","content":"hi"}}` + "\n"
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	loc := LocatePi("", sid, ws)
	if loc.JSONL != p {
		t.Fatalf("by sid+cwd: %q", loc.JSONL)
	}
	latest := LocatePi("", "", ws)
	if latest.JSONL != p || latest.SessionID != sid {
		t.Fatalf("latest: %+v", latest)
	}
	bySID := LocatePi("", sid, "")
	if bySID.JSONL != p {
		t.Fatalf("walk: %q", bySID.JSONL)
	}
	if PeekPiCWD(p) != ws {
		t.Fatal(PeekPiCWD(p))
	}
	tr := LocatePi("/tmp/x.jsonl", "", "/ws")
	if tr.JSONL != "/tmp/x.jsonl" || tr.SessionID != "x" {
		t.Fatalf("%+v", tr)
	}
	if LocatePi("", "", "").JSONL != "" {
		t.Fatal("empty")
	}
}

func TestWritePiAndOpenCodeInstall(t *testing.T) {
	home := t.TempDir()
	p, err := WritePiExtension(home, "/bin/am")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(p)
	if !strings.Contains(string(b), "hook-pi") || !strings.Contains(string(b), "turn_end") {
		t.Fatal(string(b))
	}
	if !strings.Contains(string(b), "spawnSync") || !strings.Contains(string(b), "session_before_compact") {
		t.Fatal("compact must wait: " + string(b))
	}
	o, err := WriteOpenCodePlugin(home)
	if err != nil {
		t.Fatal(err)
	}
	b, _ = os.ReadFile(o)
	if !strings.Contains(string(b), "session.idle") || !strings.Contains(string(b), "experimental.session.compacting") {
		t.Fatal(string(b))
	}
	src := OpenCodePluginSource()
	if !strings.Contains(src, "session.idle") || strings.Contains(src, "AGENT_MEMORY_URL") {
		t.Fatal("plugin must talk to sidecar only, not home URL")
	}
	if !strings.Contains(src, "AbortSignal.timeout") {
		t.Fatal("plugin fetch must time out")
	}
	if !strings.Contains(src, "5000") {
		t.Fatal("compact fetch must wait longer than a turn")
	}
	if PiExtensionSource("/x") == "" {
		t.Fatal("empty source")
	}
}

func TestLocateOpenCode(t *testing.T) {
	t.Setenv("OPENCODE_HOME", "/tmp/oc")
	t.Setenv("OPENCODE_DB", "/tmp/oc/opencode.db")
	if OpenCodeDB() != "/tmp/oc/opencode.db" {
		t.Fatal(OpenCodeDB())
	}
	loc := LocateOpenCode("ses_1", "/ws")
	if loc.SessionID != "ses_1" || loc.CWD != "/ws" || loc.JSONL != "" {
		t.Fatalf("%+v", loc)
	}
}
