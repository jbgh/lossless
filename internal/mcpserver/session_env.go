package mcpserver

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"lossless/internal/retrieve"
)

// harnessID matches the session ids harnesses actually issue: a UUID
// (Claude, Codex, Grok, Pi), OpenCode's ses_…, and a Claude subagent's
// agent-<hex>. A UUID with a suffix glued on (a Pi task pid, a tape
// .partN) keeps the UUID.
var (
	harnessID  = regexp.MustCompile(`^(?:[0-9A-Fa-f]{8}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{12}|ses_[A-Za-z0-9]{20,}|agent-[0-9a-f]{8,})$`)
	suffixedID = regexp.MustCompile(`^([0-9A-Fa-f]{8}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{12})[-.]`)
)

// fillSession cleans a caller-sent id and fills the harness session id
// when the caller omitted one or sent one no harness issued (w22-ios,
// adversarial-verify-fix-delivery): a model-invented name would book a
// phantom session. The env fill: LOSSLESS_SESSION_ID (explicit wrapper),
// then CLAUDE_CODE_SESSION_ID when this process's parent is Claude Code
// itself (every process under a Claude shell inherits the id, so a Pi or
// OpenCode started there must not take it; a nested `claude -p` is its
// own parent and books its own session), then PI_SESSION_ID. Claude
// subagents share the parent's MCP server and so book to the parent
// session, which is narrower than the project-wide default. Pi
// scrubs PI_SESSION_ID from MCP servers, so Pi and OpenCode get the id
// from the lossless extension/plugin, which stamps it on the tool call.
// Never invent: absent stays omitted.
func fillSession(sent string) string {
	if s := retrieve.CleanSessionID(sent); s != "" {
		if harnessID.MatchString(s) {
			return s
		}
		if m := suffixedID.FindStringSubmatch(s); m != nil {
			return m[1]
		}
	}
	if s := retrieve.CleanSessionID(os.Getenv("LOSSLESS_SESSION_ID")); s != "" {
		return s
	}
	if s := retrieve.CleanSessionID(os.Getenv("CLAUDE_CODE_SESSION_ID")); s != "" && filepath.Base(parentComm()) == "claude" {
		return s
	}
	return retrieve.CleanSessionID(os.Getenv("PI_SESSION_ID"))
}

// parentComm is the parent process's command name (a test seam).
var parentComm = func() string {
	parentOnce.Do(func() { parentName = readParentComm(os.Getppid()) })
	return parentName
}

var (
	parentOnce sync.Once
	parentName string
)

func readParentComm(pid int) string {
	if b, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/comm"); err == nil {
		return strings.TrimSpace(string(b))
	}
	out, err := exec.Command("ps", "-o", "comm=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
