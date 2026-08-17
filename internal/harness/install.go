package harness

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func GrokHookCommand(exe string) string {
	return fmt.Sprintf("%q hook-grok", exe)
}

func ClaudeHookCommand(exe string) string {
	return fmt.Sprintf("%q hook-claude", exe)
}

func CodexHookCommand(exe string) string {
	return fmt.Sprintf("%q hook-codex", exe)
}

func GrokHookFile(exe string) []byte {
	cmd := GrokHookCommand(exe)
	q, _ := json.Marshal(cmd)
	body := fmt.Sprintf(`{
  "hooks": {
    "PreCompact": [{ "hooks": [{ "type": "command", "command": %s, "timeout": 6 }] }],
    "PostCompact": [{ "hooks": [{ "type": "command", "command": %s, "timeout": 6 }] }],
    "Stop": [{ "hooks": [{ "type": "command", "command": %s, "timeout": 2 }] }],
    "SessionEnd": [{ "hooks": [{ "type": "command", "command": %s, "timeout": 4 }] }]
  }
}
`, q, q, q, q)
	return []byte(body)
}

func WriteGrokHooks(home, exe string) (string, error) {
	destDir := filepath.Join(home, ".grok", "hooks")
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return "", err
	}
	dest := filepath.Join(destDir, "lossless.json")
	if err := writeUserConfig(dest, GrokHookFile(exe), 0o644); err != nil {
		return "", err
	}
	return dest, nil
}

func MergeClaudeSettings(existing []byte, exe string) ([]byte, error) {
	root := map[string]any{}
	trim := bytes.TrimSpace(existing)
	if len(trim) > 0 {
		if err := json.Unmarshal(trim, &root); err != nil {
			return nil, err
		}
	}
	hooks, _ := root["hooks"].(map[string]any)
	if hooks == nil {
		hooks = map[string]any{}
		root["hooks"] = hooks
	}
	cmd := ClaudeHookCommand(exe)
	for ev, timeout := range map[string]int{"PreCompact": 6, "PostCompact": 6, "Stop": 2, "SessionEnd": 4} {
		if err := ensureClaudeEvent(hooks, ev, cmd, timeout); err != nil {
			return nil, err
		}
	}
	out, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(out, '\n'), nil
}

func ensureClaudeEvent(hooks map[string]any, event, command string, timeout int) error {
	if containsCommand(hooks[event], command) {
		return nil
	}
	entry := map[string]any{
		"hooks": []any{
			map[string]any{"type": "command", "command": command, "timeout": timeout},
		},
	}
	switch cur := hooks[event].(type) {
	case nil:
		hooks[event] = []any{entry}
	case []any:
		hooks[event] = append(cur, entry)
	default:
		hooks[event] = []any{entry}
	}
	return nil
}

func containsCommand(v any, command string) bool {
	b, err := json.Marshal(v)
	if err != nil {
		return false
	}
	s := string(b)
	if strings.Contains(s, "hook-claude") || strings.Contains(s, "hook-codex") || strings.Contains(s, command) {
		return true
	}
	// JSON string escaping turns `"exe" hook-claude` into \"exe\" hook-claude
	return strings.Contains(s, strings.ReplaceAll(command, `"`, `\"`))
}

func InstallHooks(home, exe string) ([]string, error) {
	if home == "" {
		return nil, fmt.Errorf("home required")
	}
	if exe == "" {
		return nil, fmt.Errorf("executable required")
	}
	var out []string
	writers := []func() (string, error){
		func() (string, error) { return WriteGrokHooks(home, exe) },
		func() (string, error) { return WriteClaudeHooks(home, exe) },
		func() (string, error) { return WriteCodexHooks(home, exe) },
		func() (string, error) { return WriteCodexFeatures(home) },
		func() (string, error) { return WritePiExtension(home, exe) },
		func() (string, error) { return WriteOpenCodePlugin(home) },
	}
	for _, w := range writers {
		p, err := w()
		if err != nil {
			return out, err
		}
		out = append(out, p)
	}
	skills, err := InstallSkills(home)
	if err != nil {
		return out, err
	}
	out = append(out, skills...)
	return out, nil
}

func MergeCodexHooks(existing []byte, exe string) ([]byte, error) {
	root := map[string]any{}
	trim := bytes.TrimSpace(existing)
	if len(trim) > 0 {
		if err := json.Unmarshal(trim, &root); err != nil {
			return nil, err
		}
	}
	hooks, _ := root["hooks"].(map[string]any)
	if hooks == nil {
		hooks = map[string]any{}
		root["hooks"] = hooks
	}
	cmd := CodexHookCommand(exe)
	for ev, timeout := range map[string]int{"PreCompact": 6, "PostCompact": 6, "Stop": 2, "SessionEnd": 3} {
		if err := ensureClaudeEvent(hooks, ev, cmd, timeout); err != nil {
			return nil, err
		}
	}
	out, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(out, '\n'), nil
}

func WriteCodexHooks(home, exe string) (string, error) {
	dest := filepath.Join(home, ".codex", "hooks.json")
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return "", err
	}
	var existing []byte
	if b, err := os.ReadFile(dest); err == nil {
		existing = b
	}
	merged, err := MergeCodexHooks(existing, exe)
	if err != nil {
		return "", err
	}
	if err := writeUserConfig(dest, merged, 0o600); err != nil {
		return "", err
	}
	return dest, nil
}

func PiExtensionSource(exe string) string {
	return `import type { ExtensionAPI } from "@earendil-works/pi-coding-agent";
import { spawn, spawnSync } from "node:child_process";

const exe = ` + jsonQuote(exe) + `;

function fire(ctx: { cwd: string; sessionManager: { getSessionFile(): string | undefined; getSessionId(): string } }, source: string) {
  const file = ctx.sessionManager.getSessionFile();
  if (!file) return;
  const payload = JSON.stringify({
    session_id: ctx.sessionManager.getSessionId(),
    transcript_path: file,
    cwd: ctx.cwd,
    hook_event_name: source,
  });
  try {
    if (source === "compact") {
      spawnSync(exe, ["hook-pi"], {
        input: payload,
        timeout: 5000,
        stdio: ["pipe", "ignore", "ignore"],
      });
      return;
    }
    const child = spawn(exe, ["hook-pi"], { stdio: ["pipe", "ignore", "ignore"] });
    child.stdin?.end(payload);
    child.unref();
  } catch {
    // fail-open
  }
}

export default function (pi: ExtensionAPI) {
  pi.on("turn_end", (_event, ctx) => fire(ctx, "turn"));
  pi.on("agent_settled", (_event, ctx) => fire(ctx, "turn"));
  pi.on("session_before_compact", (_event, ctx) => fire(ctx, "compact"));
  pi.on("session_shutdown", (_event, ctx) => fire(ctx, "session_end"));
}
`
}

func jsonQuote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func WritePiExtension(home, exe string) (string, error) {
	dest := filepath.Join(home, ".pi", "agent", "extensions", "lossless.ts")
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return "", err
	}
	if err := writeUserConfig(dest, []byte(PiExtensionSource(exe)), 0o644); err != nil {
		return "", err
	}
	return dest, nil
}

func OpenCodePluginSource() string {
	return `// lossless OpenCode plugin — fail-open catch-up to the local sidecar
export const AgentMemory = async ({ directory }) => {
  const url = (process.env.LOSSLESS_SIDECAR || "http://127.0.0.1:7432").replace(/\/$/, "");
  const token = process.env.LOSSLESS_TOKEN || "";
  const fire = async (sessionID, source) => {
    if (!sessionID) return;
    try {
      const headers = { "Content-Type": "application/json" };
      if (token) headers.Authorization = "Bearer " + token;
      const ms = source === "compact" ? 5000 : 800;
      await fetch(url + "/v1/catch-up", {
        method: "POST",
        headers,
        body: JSON.stringify({
          harness: "opencode",
          session_id: sessionID,
          workspace_root: directory,
          source,
        }),
        signal: AbortSignal.timeout(ms),
      });
    } catch {
      // fail-open
    }
  };
  return {
    event: async ({ event }) => {
      const sid = event?.properties?.sessionID || event?.properties?.sessionId || event?.properties?.info?.id;
      if (event.type === "session.idle") await fire(sid, "turn");
      if (event.type === "session.compacted") await fire(sid, "compact");
      if (event.type === "session.deleted") await fire(sid, "session_end");
    },
    "experimental.session.compacting": async (input) => {
      const sid = input?.sessionID || input?.sessionId || input?.session_id;
      await fire(sid, "compact");
    },
  };
};
`
}

func WriteOpenCodePlugin(home string) (string, error) {
	cfg := os.Getenv("OPENCODE_CONFIG")
	if cfg == "" {
		cfg = filepath.Join(home, ".config", "opencode")
	}
	dest := filepath.Join(cfg, "plugins", "lossless.ts")
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return "", err
	}
	if err := writeUserConfig(dest, []byte(OpenCodePluginSource()), 0o644); err != nil {
		return "", err
	}
	return dest, nil
}

func MergeCodexFeatures(existing []byte) []byte {
	if bytes.Contains(existing, []byte("hooks = true")) || bytes.Contains(existing, []byte("codex_hooks = true")) {
		return existing
	}
	var b bytes.Buffer
	if len(bytes.TrimSpace(existing)) > 0 {
		b.Write(existing)
		if !bytes.HasSuffix(existing, []byte("\n")) {
			b.WriteByte('\n')
		}
		b.WriteByte('\n')
	}
	// Hooks are on by default; keep the canonical key so a later [features]
	// block that only sets other flags does not surprise us.
	if !bytes.Contains(existing, []byte("[features]")) {
		b.WriteString("[features]\nhooks = true\n")
	}
	return b.Bytes()
}

func WriteCodexFeatures(home string) (string, error) {
	dest := filepath.Join(home, ".codex", "config.toml")
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return "", err
	}
	var existing []byte
	if b, err := os.ReadFile(dest); err == nil {
		existing = b
	}
	if err := writeUserConfig(dest, MergeCodexFeatures(existing), 0o600); err != nil {
		return "", err
	}
	return dest, nil
}

func WriteClaudeHooks(home, exe string) (string, error) {
	dest := filepath.Join(home, ".claude", "settings.json")
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return "", err
	}
	var existing []byte
	if b, err := os.ReadFile(dest); err == nil {
		existing = b
	}
	merged, err := MergeClaudeSettings(existing, exe)
	if err != nil {
		return "", err
	}
	if err := writeUserConfig(dest, merged, 0o600); err != nil {
		return "", err
	}
	return dest, nil
}
