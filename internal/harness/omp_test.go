package harness

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeOmpSession writes a session file the way omp does: a fixed-width
// 256-byte title slot, then the header, then entries.
func writeOmpSession(t *testing.T, path, id, cwd string, extra ...string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	slot, _ := json.Marshal(map[string]any{"type": "title", "v": 1, "title": "t", "pad": strings.Repeat(" ", 200)})
	header, _ := json.Marshal(map[string]any{"type": "session", "version": 3, "id": id, "cwd": cwd})
	var b strings.Builder
	b.Write(slot)
	b.WriteString("\n")
	b.Write(header)
	b.WriteString("\n")
	for _, line := range extra {
		b.WriteString(line)
		b.WriteString("\n")
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
}

// clearOmpEnv pins omp's path env vars, so a developer's own OMP_HOME or
// PI_CODING_AGENT_DIR cannot move the files a test writes.
func clearOmpEnv(t *testing.T) {
	t.Helper()
	t.Setenv("OMP_HOME", "")
	t.Setenv("PI_CONFIG_DIR", "")
	t.Setenv("PI_CODING_AGENT_DIR", "")
}

func ompTestEnv(t *testing.T) (home string) {
	t.Helper()
	home = t.TempDir()
	t.Setenv("HOME", home)
	clearOmpEnv(t)
	return home
}

func TestOmpAgentDirFollowsOmpEnv(t *testing.T) {
	home := ompTestEnv(t)
	if got, want := OmpAgentDir(home), filepath.Join(home, ".omp", "agent"); got != want {
		t.Fatalf("agent dir %q want %q", got, want)
	}
	t.Setenv("PI_CONFIG_DIR", "/cfg")
	if got, want := OmpAgentDir(home), "/cfg/agent"; got != want {
		t.Fatalf("PI_CONFIG_DIR %q want %q", got, want)
	}
	t.Setenv("PI_CODING_AGENT_DIR", "/agentdir")
	if got, want := OmpAgentDir(home), "/agentdir"; got != want {
		t.Fatalf("PI_CODING_AGENT_DIR %q want %q", got, want)
	}
	t.Setenv("PI_CODING_AGENT_DIR", "")
	t.Setenv("OMP_HOME", filepath.Join(home, ".omp2"))
	if got, want := OmpAgentDir(home), filepath.Join(home, ".omp2", "agent"); got != want {
		t.Fatalf("OMP_HOME %q want %q", got, want)
	}
}

func TestOmpSessionSlugBranches(t *testing.T) {
	home := ompTestEnv(t)
	if got, want := OmpSessionSlug(filepath.Join(home, "developer", "lossless")), "-developer-lossless"; got != want {
		t.Fatalf("home slug %q want %q", got, want)
	}
	// Not under home and not under the temp root: the absolute-encoded form.
	if got, want := OmpSessionSlug("/opt/build/api"), "--opt-build-api--"; got != want {
		t.Fatalf("absolute slug %q want %q", got, want)
	}
	// t.TempDir() lives under the temp root, which has its own prefix.
	dir := t.TempDir()
	rel, err := filepath.Rel(filepath.Clean(os.TempDir()), dir)
	if err != nil || strings.HasPrefix(rel, "..") {
		t.Skipf("temp dir %q is not under %q", dir, os.TempDir())
	}
	if got, want := OmpSessionSlug(dir), "-tmp-"+strings.ReplaceAll(rel, string(filepath.Separator), "-"); got != want {
		t.Fatalf("temp slug %q want %q", got, want)
	}
}

func TestLocateOmpAndPeekCWD(t *testing.T) {
	home := ompTestEnv(t)
	cwd := filepath.Join(home, "developer", "lossless")
	const sid = "01a130e9-6160-70f1-915d-4a727bf69ef9"
	path := filepath.Join(OmpSessionsRoot(), OmpSessionSlug(cwd), "2026-10-12T16-19-23-616Z_"+sid+".jsonl")
	writeOmpSession(t, path, sid, cwd, `{"type":"message","message":{"role":"user","content":[{"type":"text","text":"go"}]}}`)

	if got := PeekOmpCWD(path); got != cwd {
		t.Fatalf("peek cwd %q want %q", got, cwd)
	}
	if loc := LocateOmp("", sid, cwd); loc.JSONL != path || loc.SessionID != sid {
		t.Fatalf("locate by cwd+id: %+v", loc)
	}
	// The bucket name is not reversible; an id alone still finds the tape
	// and reads the cwd out of the header.
	byID := LocateOmp("", sid, "")
	if byID.JSONL != path || byID.CWD != cwd {
		t.Fatalf("locate by id: %+v", byID)
	}
	if loc := LocateOmp(path, "", ""); loc.JSONL != path || loc.SessionID != sid {
		t.Fatalf("locate from transcript: %+v", loc)
	}
	// Legacy files that start at the header still peek.
	legacy := filepath.Join(OmpSessionsRoot(), OmpSessionSlug(cwd), "2026-10-12T16-19-24-616Z_01a130e9-6160-70f1-915d-4a727bf69efa.jsonl")
	if err := os.WriteFile(legacy, []byte(`{"type":"session","version":2,"id":"legacy","cwd":"`+cwd+`"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := PeekOmpCWD(legacy); got != cwd {
		t.Fatalf("legacy peek %q", got)
	}
	if _, sid := latestOmp(filepath.Dir(legacy)); sid == "" {
		t.Fatal("latestOmp found nothing")
	}
}

func TestOmpSessionFileRejectsArtifacts(t *testing.T) {
	cases := []struct {
		base string
		want bool
	}{
		{"2026-10-12T16-19-23-616Z_01a130e9-6160-70f1-915d-4a727bf69ef9.jsonl", true},
		{"2026-10-12T16-19-23-616Z_01a130e9-6160-70f1-915d-4a727bf69ef9.jsonl.zst", false},
		// Subagent transcripts and bash logs live beside the tape, under a
		// directory named after it.
		{"RepoPlumbing.jsonl", false},
		{"3.bash.log", false},
		{".2026-10-12T16-19-23-616Z_01a130e9-6160-70f1-915d-4a727bf69ef9.jsonl.lock.os", false},
		{".2026-10-12T16-19-23-616Z_01a130e9-6160-70f1-915d-4a727bf69ef9.jsonl.123.tmp", false},
	}
	for _, c := range cases {
		if got := OmpSessionFile(filepath.Join("/sessions/bucket", c.base)); got != c.want {
			t.Errorf("OmpSessionFile(%q) = %v want %v", c.base, got, c.want)
		}
	}
}

func TestWriteOmpExtensionAndMCP(t *testing.T) {
	home := ompTestEnv(t)
	ext, err := WriteOmpExtension(home, "/bin/am")
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(home, ".omp", "agent", "extensions", "lossless.ts"); ext != want {
		t.Fatalf("extension %q want %q", ext, want)
	}
	body, err := os.ReadFile(ext)
	if err != nil {
		t.Fatal(err)
	}
	src := string(body)
	for _, need := range []string{
		`"@oh-my-pi/pi-coding-agent"`,
		`const exe = "/bin/am";`,
		`"hook-omp"`,
		`pi.on("turn_end"`,
		`pi.on("session_before_compact"`,
		`pi.on("session_shutdown"`,
		`xd:\/\/`, // the device-path stamp
		`stampLossless`,
	} {
		if !strings.Contains(src, need) {
			t.Errorf("extension source missing %q", need)
		}
	}

	// The device stamp must recognize the path omp actually shows and
	// rewrite the JSON content string.
	if !strings.Contains(src, `(?:ask|remember|get_record)`) || !strings.Contains(src, `input.content = JSON.stringify(args)`) {
		t.Error("device stamp does not rewrite xd:// lossless calls")
	}
	// A print-mode session can exit right after turn_end, so the catch-up
	// child must not be in omp's process group.
	if !strings.Contains(src, "detached: true") {
		t.Error("the turn catch-up must survive omp exiting right after the event")
	}
	if !strings.Contains(src, "spawnSync") {
		t.Error("session_before_compact must wait for the tape")
	}

	// MCP merges into the native file and keeps what is already there.
	mcpPath := filepath.Join(home, ".omp", "agent", "mcp.json")
	if err := os.MkdirAll(filepath.Dir(mcpPath), 0o755); err != nil {
		t.Fatal(err)
	}
	existing := `{"$schema":"https://example/schema.json","mcpServers":{"other":{"command":"x"}}}`
	if err := os.WriteFile(mcpPath, []byte(existing), 0o600); err != nil {
		t.Fatal(err)
	}
	dest, err := WriteOmpMCP(home, "/bin/am", nil)
	if err != nil {
		t.Fatal(err)
	}
	if dest != mcpPath {
		t.Fatalf("mcp dest %q want %q", dest, mcpPath)
	}
	raw, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	var root map[string]any
	if err := json.Unmarshal(raw, &root); err != nil {
		t.Fatal(err)
	}
	if root["$schema"] != "https://example/schema.json" {
		t.Error("merge dropped $schema")
	}
	servers, _ := root["mcpServers"].(map[string]any)
	if _, ok := servers["other"]; !ok {
		t.Error("merge dropped another server")
	}
	entry, _ := servers["lossless"].(map[string]any)
	if entry == nil || entry["command"] != "/bin/am" {
		t.Fatalf("lossless entry %v", servers["lossless"])
	}
	if args, _ := entry["args"].([]any); len(args) != 1 || args[0] != "mcp" {
		t.Fatalf("lossless args %v", entry["args"])
	}
}

func TestOmpSkillAndRuleDests(t *testing.T) {
	home := ompTestEnv(t)
	if got, want := SkillDests(home)["omp"], filepath.Join(home, ".omp", "agent", "skills", "lossless", "SKILL.md"); got != want {
		t.Fatalf("skill dest %q want %q", got, want)
	}
	if got, want := RuleDests(home)["omp"], filepath.Join(home, ".omp", "agent", "RULES.md"); got != want {
		t.Fatalf("rule dest %q want %q", got, want)
	}
}
