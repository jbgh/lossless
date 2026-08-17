// Package inspect reports tape vs claims vs last ask packs.
package inspect

import (
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"

	"lossless/internal/claim"
	"lossless/internal/projectkey"
	"lossless/internal/retrieve"
	"lossless/internal/store"
)

type Report struct {
	Home     string               `json:"home"`
	Records  int                  `json:"records"`
	Vectors  int                  `json:"vectors"`
	Embedder string               `json:"embedder"`
	Projects []store.ProjectStats `json:"projects"`
	Cursors  []store.CursorRow    `json:"cursors,omitempty"`
	Detail   *ProjectDetail       `json:"detail,omitempty"`
	Ask      *AskView             `json:"ask,omitempty"`
}

type ProjectDetail struct {
	Key      string          `json:"key"`
	ByType   map[string]int  `json:"by_type"`
	Sessions []store.Session `json:"sessions"`
	Recent   []claim.Record  `json:"recent"`
	LastAsks []AskPack       `json:"last_asks"`
}

type AskPack struct {
	At        string    `json:"at"`
	SessionID string    `json:"session_id"`
	Paths     []string  `json:"paths"`
	Tokens    []string  `json:"tokens"`
	Hits      []HitView `json:"hits"`
}

type HitView struct {
	ID      string   `json:"id"`
	Type    string   `json:"type"`
	Text    string   `json:"text"`
	When    string   `json:"when,omitempty"`
	Paths   []string `json:"paths,omitempty"`
	AgeDays float64  `json:"age_days,omitempty"`
	Path    float64  `json:"path,omitempty"`
	Why     string   `json:"why,omitempty"`
}

type AskView struct {
	Project  string    `json:"project"`
	Warnings []string  `json:"warnings"`
	Hits     []HitView `json:"hits"`
}

func Build(st *store.Store, project string) (Report, error) {
	rep := Report{
		Home:     st.Root,
		Records:  st.CountActive(),
		Vectors:  st.VectorCount(),
		Embedder: st.EmbedderName(),
	}
	if rep.Embedder == "" {
		rep.Embedder = "none"
	}
	stats, err := st.ListProjectStats()
	if err != nil {
		return Report{}, err
	}
	rep.Projects = stats
	curs, err := st.ListCursors()
	if err != nil {
		return Report{}, err
	}
	if project == "" {
		rep.Cursors = curs
		return rep, nil
	}
	key := projectkey.Normalize(project)
	if key == "" {
		key = project
	}
	det := &ProjectDetail{Key: key}
	det.ByType, err = st.CountByType(key)
	if err != nil {
		return Report{}, err
	}
	allSess, err := st.ListSessions()
	if err != nil {
		return Report{}, err
	}
	for _, s := range allSess {
		if s.Project == key {
			det.Sessions = append(det.Sessions, s)
		}
	}
	det.Recent, err = st.ListRecentActive(key, 8)
	if err != nil {
		return Report{}, err
	}
	acts, err := st.RecentActions(key, "", 40)
	if err != nil {
		return Report{}, err
	}
	det.LastAsks = packsFromActions(st, acts, 3)
	for _, c := range curs {
		if sessionMatches(det.Sessions, c.Path) {
			rep.Cursors = append(rep.Cursors, c)
		}
	}
	rep.Detail = det
	return rep, nil
}

func Ask(st *store.Store, req retrieve.Request, now time.Time) (*AskView, error) {
	eng := retrieve.Engine{Store: st}
	if !now.IsZero() {
		eng.Now = func() time.Time { return now }
	}
	out, err := eng.Ask(req)
	if err != nil {
		return nil, err
	}
	view := &AskView{Project: out.Project, Warnings: out.Warnings}
	for _, h := range out.Context {
		view.Hits = append(view.Hits, describe(st, h, req.Paths, now))
	}
	return view, nil
}

func describe(st *store.Store, h retrieve.Hit, askPaths []string, now time.Time) HitView {
	v := HitView{ID: h.ID, Type: h.Type, Text: h.Text, When: h.When, Paths: h.Paths}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if t, err := time.Parse(time.RFC3339, h.When); err == nil {
		v.AgeDays = now.Sub(t).Hours() / 24
		if v.AgeDays < 0 {
			v.AgeDays = 0
		}
	}
	if rec, ok := st.Get(h.ID); ok {
		v.Path = jaccard(claim.PathKeys(askPaths), claim.PathKeys(rec.Paths))
	}
	v.Why = why(h.Type, v.Path, v.AgeDays)
	return v
}

func why(typ string, path, age float64) string {
	var bits []string
	bits = append(bits, typ)
	if path > 0 {
		bits = append(bits, fmt.Sprintf("path=%.2f", path))
	} else {
		bits = append(bits, "path=0")
	}
	if age >= 1 {
		bits = append(bits, fmt.Sprintf("age=%.0fd", age))
	}
	return strings.Join(bits, " ")
}

func packsFromActions(st *store.Store, acts []store.Action, limit int) []AskPack {
	type key struct{ at, sess string }
	order := []key{}
	by := map[key]*AskPack{}
	for _, a := range acts {
		if a.Kind != store.ActionAsk {
			continue
		}
		k := key{a.At, a.SessionID}
		p := by[k]
		if p == nil {
			p = &AskPack{At: a.At, SessionID: a.SessionID, Paths: a.Paths, Tokens: a.Tokens}
			by[k] = p
			order = append(order, k)
		}
		if a.ClaimID == "" {
			continue
		}
		h := HitView{ID: a.ClaimID}
		if rec, ok := st.Get(a.ClaimID); ok {
			h.Type = rec.Type
			h.Text = rec.Text
			h.When = rec.CreatedAt
			h.Paths = rec.Paths
		}
		p.Hits = append(p.Hits, h)
	}
	if limit > 0 && len(order) > limit {
		order = order[:limit]
	}
	out := make([]AskPack, 0, len(order))
	for _, k := range order {
		out = append(out, *by[k])
	}
	return out
}

func sessionMatches(sess []store.Session, path string) bool {
	for _, s := range sess {
		if s.JSONL == path {
			return true
		}
	}
	return false
}

func jaccard(a, b []string) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	as := map[string]bool{}
	for _, x := range a {
		if x != "" {
			as[x] = true
		}
	}
	inter := 0
	for _, x := range b {
		if x != "" && as[x] {
			inter++
		}
	}
	union := len(as)
	seen := map[string]bool{}
	for _, x := range b {
		if x == "" || seen[x] {
			continue
		}
		seen[x] = true
		if !as[x] {
			union++
		}
	}
	if union == 0 {
		return 0
	}
	return float64(inter) / float64(union)
}

func Format(w io.Writer, r Report) {
	fmt.Fprintf(w, "lossless inspect  %s\n", r.Home)
	fmt.Fprintf(w, "records %d   vectors %d   embedder %s   projects %d\n",
		r.Records, r.Vectors, r.Embedder, len(r.Projects))
	if r.Detail == nil {
		fmt.Fprintln(w)
		fmt.Fprintf(w, "%-22s %6s %4s %4s %4s %4s %5s %8s  %s\n",
			"PROJECT", "CLAIMS", "F", "D", "C", "S", "SESS", "RAW", "CURSORS")
		for _, p := range r.Projects {
			fmt.Fprintf(w, "%-22s %6d %4d %4d %4d %4d %5d %8s  %s\n",
				clip(p.Key, 22), p.Active, p.ByType["failed"], p.ByType["decision"],
				p.ByType["constraint"], p.ByType["state"], p.Sessions, bytes(p.RawBytes),
				cursorSummary(r.Cursors, p))
		}
		nPast, nBehind := 0, 0
		for _, c := range r.Cursors {
			switch c.Status {
			case "past-eof":
				nPast++
			case "behind":
				nBehind++
			}
		}
		if nPast+nBehind > 0 {
			fmt.Fprintf(w, "\ncursors: %d past-eof  %d behind  (catch-up not even with the tape)\n", nPast, nBehind)
		}
		return
	}
	d := r.Detail
	fmt.Fprintf(w, "\nproject %s\n", d.Key)
	fmt.Fprintf(w, "  types  failed=%d decision=%d constraint=%d state=%d\n",
		d.ByType["failed"], d.ByType["decision"], d.ByType["constraint"], d.ByType["state"])
	if len(d.Sessions) == 0 {
		fmt.Fprintln(w, "  sessions  (none recorded)")
	}
	for _, s := range d.Sessions {
		cur := "no-cursor"
		for _, c := range r.Cursors {
			if c.Path == s.JSONL {
				cur = fmt.Sprintf("%s %s/%s", c.Status, bytes(c.Cursor), bytes(c.Size))
				break
			}
		}
		fmt.Fprintf(w, "  session  %s  %s  %s  %s\n", s.Harness, s.SessionID, filepath.Base(s.JSONL), cur)
	}
	if len(d.Recent) > 0 {
		fmt.Fprintln(w, "\nrecent claims (stored; ask still drops extract noise)")
		for _, c := range d.Recent {
			fmt.Fprintf(w, "  [%s] %s  %s\n", c.Type, c.CreatedAt, clip(c.Text, 88))
		}
	}
	if len(d.LastAsks) > 0 {
		fmt.Fprintln(w, "\nlast asks")
		for _, a := range d.LastAsks {
			fmt.Fprintf(w, "  %s  session=%s  paths=%s\n", a.At, a.SessionID, strings.Join(a.Paths, ","))
			if len(a.Hits) == 0 {
				fmt.Fprintln(w, "    (empty pack)")
			}
			for i, h := range a.Hits {
				fmt.Fprintf(w, "    %d [%s] %s\n", i+1, h.Type, clip(h.Text, 88))
			}
		}
	}
	if r.Ask != nil {
		fmt.Fprintln(w, "\nlive ask")
		if len(r.Ask.Warnings) > 0 {
			for _, wmsg := range r.Ask.Warnings {
				fmt.Fprintf(w, "  warn  %s\n", wmsg)
			}
		}
		if len(r.Ask.Hits) == 0 {
			fmt.Fprintln(w, "  (empty pack)")
		}
		for i, h := range r.Ask.Hits {
			fmt.Fprintf(w, "  %d [%s] %s\n      %s\n", i+1, h.Type, h.Why, clip(h.Text, 100))
		}
	}
}

func cursorSummary(curs []store.CursorRow, p store.ProjectStats) string {
	// Overview: count statuses that belong to this project's sessions is
	// unknown here; print raw-file count as a hint, plus global flags if one project.
	if p.Sessions == 0 {
		return "—"
	}
	return fmt.Sprintf("%d sess", p.Sessions)
}

func bytes(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1fM", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1fK", float64(n)/(1<<10))
	case n <= 0:
		return "0"
	default:
		return fmt.Sprintf("%dB", n)
	}
}

func clip(s string, n int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
