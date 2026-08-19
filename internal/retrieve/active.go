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

// CompactSource is PreCompact / PostCompact / session compacting.
// Stop / turn must not write the active file.
func CompactSource(source string) bool {
	return strings.Contains(strings.ToLower(strings.TrimSpace(source)), "compact")
}

// RefreshActive runs a real ask after compact catch-up and writes
// ~/.lossless/active/<owner__repo>.md. Fail-open. Not an inject.
func RefreshActive(st *store.Store, home string, req write.CatchUpRequest) {
	if st == nil || !CompactSource(req.Source) {
		return
	}
	defer func() { _ = recover() }()
	if home == "" {
		home = st.Root
	}
	out, err := Ask(st, Request{
		Project:       req.Project,
		WorkspaceRoot: req.WorkspaceRoot,
		SessionID:     req.SessionID,
	})
	if err != nil {
		return
	}
	_ = WriteActive(home, out, time.Now().UTC())
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
	if len(out.Warnings) > 0 {
		b.WriteString("\n## warnings\n")
		for _, w := range out.Warnings {
			fmt.Fprintf(&b, "- %s\n", w)
		}
	}
	if len(out.Context) > 0 {
		b.WriteString("\n## context\n")
		for _, h := range out.Context {
			fmt.Fprintf(&b, "\n### %s\n> %s\n", h.Type, h.Text)
		}
	}
	return b.String()
}
