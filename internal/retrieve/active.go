package retrieve

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
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
	// Before the ask: what this session said before the compact is no
	// longer in its window, so it is memory again (SaidEchoMinutes).
	// The session is named by its tape, as catch-up named it: the hook may
	// have sent no session id at all.
	if key, err := resolveProject(Request{Project: req.Project, WorkspaceRoot: req.WorkspaceRoot}); err == nil {
		markCompact(st, key, tapeSession(rawPath, req.SessionID))
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

var tapePartRE = regexp.MustCompile(`\.part\d+$`)

// tapeSession is the session a raw tape belongs to, from its file name;
// fallback when there is no tape path.
func tapeSession(rawPath, fallback string) string {
	if rawPath == "" {
		return fallback
	}
	name := strings.TrimSuffix(strings.TrimSuffix(filepath.Base(rawPath), ".zst"), ".jsonl")
	if name = tapePartRE.ReplaceAllString(name, ""); name == "" {
		return fallback
	}
	return name
}

// compactMarker is one small file per compacted session: a compact in
// one session says nothing about what another still has in its window.
// Sessions are keyed the way their tapes are named, so the id a hook
// sent, the one catch-up derived, and the one an ask sends all meet.
func compactMarker(st *store.Store, project, session string) string {
	key := strings.TrimSuffix(filepath.Base(st.RawPath(project, session, time.Time{})), ".jsonl")
	sum := sha256.Sum256([]byte(key))
	return filepath.Join(st.Root, "active", "compacted", projectkey.Encode(project)+"."+hex.EncodeToString(sum[:8]))
}

// markCompact records that session was just compacted.
func markCompact(st *store.Store, project, session string) {
	if st == nil || project == "" || CleanSessionID(session) == "" {
		return
	}
	p := compactMarker(st, project, session)
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return
	}
	_ = os.WriteFile(p, []byte(time.Now().UTC().Format(time.RFC3339)+"\n"), 0o600)
}

// compactedAt is when session was last compacted; zero if never, or if
// the harness sent no session id.
func compactedAt(st *store.Store, project, session string) time.Time {
	if st == nil || project == "" || CleanSessionID(session) == "" {
		return time.Time{}
	}
	fi, err := os.Stat(compactMarker(st, project, session))
	if err != nil {
		return time.Time{}
	}
	return fi.ModTime()
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
