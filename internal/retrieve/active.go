package retrieve

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"lossless/internal/projectkey"
	"lossless/internal/store"
	"lossless/internal/write"
)

// CompactSource is PreCompact / session.compacting / session_before_compact.
// PostCompact / session.compacted must not write the checkout: the live
// file may already be a summary. Stop / turn must not write the active file.
func CompactSource(source string) bool {
	s := strings.ToLower(strings.TrimSpace(source))
	if s == "" {
		return false
	}
	if strings.Contains(s, "postcompact") || strings.Contains(s, "compacted") {
		return false
	}
	return strings.Contains(s, "compact")
}

// RefreshActive runs a real ask after compact catch-up and writes
// ~/.lossless/active/<owner__repo>.md. Fail-open. Not an inject.
// Hot ask reads owned raw (rawPath), not the live harness file, and
// does not catch-up that file again (compact may be rewriting it).
// Empty goal stays a cold ask.
func RefreshActive(st *store.Store, home string, req write.CatchUpRequest, rawPath string) {
	if st == nil || !CompactSource(req.Source) {
		return
	}
	defer func() { _ = recover() }()
	if home == "" {
		home = st.Root
	}
	askReq := Request{
		Project:       req.Project,
		WorkspaceRoot: req.WorkspaceRoot,
		SessionID:     req.SessionID,
		skipCatchUp:   true,
	}
	if tape := ownedTape(rawPath); tape != "" {
		goal, paths := write.CompactWorkContext(tape)
		askReq.Goal = goal
		askReq.Question = goal
		askReq.Paths = paths
	}
	out, err := Ask(st, askReq)
	if err != nil {
		return
	}
	_ = WriteActive(home, out, time.Now().UTC())
}

func ownedTape(rawPath string) string {
	if rawPath == "" {
		return ""
	}
	fi, err := os.Stat(rawPath)
	if err != nil || !fi.Mode().IsRegular() {
		return ""
	}
	return rawPath
}

func WriteActive(home string, out Response, now time.Time) error {
	if home == "" || out.Project == "" {
		return nil
	}
	if len(out.Context) == 0 && len(out.Warnings) == 0 {
		return nil
	}
	dir := filepath.Join(home, "active")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	body := FormatActive(out, now)
	dest := filepath.Join(dir, projectkey.Encode(out.Project)+".md")
	tmp := dest + ".tmp"
	if err := os.WriteFile(tmp, []byte(body), 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, dest); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

func FormatActive(out Response, now time.Time) string {
	var b strings.Builder
	b.WriteString("# lossless\n\n")
	fmt.Fprintf(&b, "project: %s\n", out.Project)
	if !now.IsZero() {
		fmt.Fprintf(&b, "when: %s\n", now.Format(time.RFC3339))
	}
	b.WriteString("\nRead this or call ask. Treat warnings as blocking unless the user overrides.\n")
	b.WriteString("Packed text is the cite. get_record that one id when has_excerpt and the sentence is not enough to act.\n")
	if len(out.Warnings) > 0 {
		b.WriteString("\n## warnings\n")
		for _, w := range out.Warnings {
			fmt.Fprintf(&b, "- %s\n", w)
		}
	}
	if len(out.Context) > 0 {
		b.WriteString("\n## context\n")
		for _, h := range out.Context {
			excerpt := "false"
			if h.HasExcerpt {
				excerpt = "true"
			}
			id := h.ID
			if id == "" {
				id = "-"
			}
			fmt.Fprintf(&b, "\n- %s `%s` has_excerpt=%s\n  > %s\n", h.Type, id, excerpt, h.Text)
		}
	}
	return b.String()
}
