package mcpserver

import (
	"os"

	"lossless/internal/retrieve"
)

// fillSession cleans a caller-sent id and fills the harness session id
// when the caller omitted one. Pi resolves PI_SESSION_ID per shell
// command and does not export it to MCP server processes, so the fill
// is best-effort: harnesses and wrappers that do export a session id
// book the real session, anything else stays on the project default.
// Never invent: absent stays omitted.
func fillSession(sent string) string {
	if s := retrieve.CleanSessionID(sent); s != "" {
		return s
	}
	for _, k := range []string{"LOSSLESS_SESSION_ID", "PI_SESSION_ID"} {
		if s := retrieve.CleanSessionID(os.Getenv(k)); s != "" {
			return s
		}
	}
	return ""
}
