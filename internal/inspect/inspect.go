// Package inspect reports tape vs claims vs last ask packs.
package inspect

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"lossless/internal/claim"
	"lossless/internal/projectkey"
	"lossless/internal/retrieve"
	"lossless/internal/store"
	"lossless/internal/write"
)

type Report struct {
	Home       string               `json:"home"`
	Records    int                  `json:"records"`
	Vectors    int                  `json:"vectors"`
	Embedder   string               `json:"embedder"`
	Projects   []store.ProjectStats `json:"projects"`
	Cursors    []store.CursorRow    `json:"cursors,omitempty"`
	CursorNote map[string]string    `json:"cursor_note,omitempty"`
	Notes      []string             `json:"notes,omitempty"`
	Detail     *ProjectDetail       `json:"detail,omitempty"`
	Ask        *AskView             `json:"ask,omitempty"`
	Extract    *ExtractView         `json:"extract,omitempty"`
	Prune      *PruneResult         `json:"prune,omitempty"`
}

type ProjectDetail struct {
	Key         string          `json:"key"`
	ByType      map[string]int  `json:"by_type"`
	Sessions    []store.Session `json:"sessions"`
	Recent      []claim.Record  `json:"recent"`
	RecentPage  map[string]bool `json:"recent_page,omitempty"`
	RecentNoise int             `json:"recent_noise"`
	LastAsks    []AskPack       `json:"last_asks"`
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
	Score   float64  `json:"score,omitempty"`
	Path    float64  `json:"path,omitempty"`
	Symbol  float64  `json:"symbol,omitempty"`
	Fail    float64  `json:"fail,omitempty"`
	Shipped float64  `json:"shipped,omitempty"`
	Why     string   `json:"why,omitempty"`
}

type AskView struct {
	Project  string    `json:"project"`
	Warnings []string  `json:"warnings"`
	Hits     []HitView `json:"hits"`
	Dropped  []HitView `json:"dropped,omitempty"`
}

type ExtractView struct {
	JSONL      string             `json:"jsonl"`
	Note       string             `json:"note,omitempty"`
	Messages   int                `json:"messages"`
	Sentences  int                `json:"sentences"`
	Drafts     int                `json:"drafts"`
	Kept       int                `json:"kept"`
	SkipCounts map[string]int     `json:"skip_counts,omitempty"`
	Claims     []claim.Record     `json:"claims,omitempty"`
	Samples    []write.SkipSample `json:"samples,omitempty"`
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
	allSess, err := st.ListSessions()
	if err != nil {
		return Report{}, err
	}
	rep.CursorNote = cursorNotes(allSess, curs)
	rep.Notes = healthNotes(stats, curs)
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
	for _, s := range allSess {
		if s.Project == key {
			det.Sessions = append(det.Sessions, s)
		}
	}
	det.Recent, err = st.ListRecentActive(key, 8)
	if err != nil {
		return Report{}, err
	}
	det.RecentPage = map[string]bool{}
	for _, c := range det.Recent {
		if retrieve.ExtractNoise(c) {
			det.RecentNoise++
		}
		if c.TranscriptRef != nil {
			created, _ := time.Parse(time.RFC3339, c.CreatedAt)
			_, det.RecentPage[c.ID] = st.ExcerptCovering(c.TranscriptRef, created)
		}
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
	tr, err := eng.Explain(req)
	if err != nil {
		return nil, err
	}
	view := &AskView{Project: tr.Project, Warnings: tr.Warnings}
	for _, h := range tr.Packed {
		view.Hits = append(view.Hits, fromTrace(h))
	}
	for _, h := range tr.Dropped {
		view.Dropped = append(view.Dropped, fromTrace(h))
	}
	return view, nil
}

func fromTrace(h retrieve.TraceHit) HitView {
	return HitView{
		ID: h.ID, Type: h.Type, Text: h.Text, When: h.When, Paths: h.Paths,
		AgeDays: h.AgeDays, Score: h.Score, Path: h.Path, Symbol: h.Symbol,
		Fail: h.Fail, Shipped: h.Shipped, Why: h.Why,
	}
}

func ExtractFile(path, project string) (*ExtractView, error) {
	data, note, err := readJSONL(path)
	if err != nil {
		return nil, err
	}
	msgs, _ := write.ParseJSONL(data, 0)
	tr := &write.ExtractTrace{}
	recs := write.Extract(msgs, write.ExtractOpts{ProjectKey: project, Source: "inspect", Trace: tr})
	return &ExtractView{
		JSONL: path, Note: note, Messages: tr.Messages, Sentences: tr.Sentences,
		Drafts: tr.Drafts, Kept: tr.Kept, SkipCounts: tr.SkipCounts,
		Claims: recs, Samples: tr.Samples,
	}, nil
}

func readJSONL(path string) (string, string, error) {
	if err := write.CheckJSONLFile(path); err != nil {
		return "", "", err
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", "", err
	}
	f, err := os.OpenFile(abs, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return "", "", err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return "", "", err
	}
	const tail = 4 << 20
	if fi.Size() <= 8<<20 {
		b, err := io.ReadAll(f)
		return string(b), "", err
	}
	if _, err := f.Seek(-tail, io.SeekEnd); err != nil {
		return "", "", err
	}
	b, err := io.ReadAll(f)
	if err != nil {
		return "", "", err
	}
	if i := bytes.IndexByte(b, '\n'); i >= 0 {
		b = b[i+1:]
	}
	return string(b), fmt.Sprintf("tailed last 4M of %s", byteSize(fi.Size())), nil
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

func Format(w io.Writer, r Report) {
	fmt.Fprintf(w, "lossless inspect  %s\n", r.Home)
	fmt.Fprintf(w, "records %d   vectors %d   embedder %s   projects %d\n",
		r.Records, r.Vectors, r.Embedder, len(r.Projects))
	if r.Prune != nil {
		fmt.Fprintf(w, "pruned  projects %d  sessions %d  records %d  noise %d\n",
			len(r.Prune.DroppedProjects), r.Prune.DroppedSessions, r.Prune.DroppedRecords, r.Prune.SupersededNoise)
		for _, k := range r.Prune.DroppedProjects {
			fmt.Fprintf(w, "  drop  %s\n", k)
		}
	}
	if r.Detail == nil {
		fmt.Fprintln(w)
		fmt.Fprintf(w, "%-22s %6s %4s %4s %4s %4s %5s %8s  %s\n",
			"PROJECT", "CLAIMS", "F", "D", "C", "S", "SESS", "RAW", "CURSORS")
		hidden, hidClaims, hidRaw := 0, 0, int64(0)
		for _, p := range r.Projects {
			if tinyPathHash(p) {
				hidden++
				hidClaims += p.Active
				hidRaw += p.RawBytes
				continue
			}
			note := r.CursorNote[p.Key]
			if note == "" {
				note = "—"
			}
			fmt.Fprintf(w, "%-22s %6d %4d %4d %4d %4d %5d %8s  %s\n",
				clip(p.Key, 22), p.Active, p.ByType["failed"], p.ByType["decision"],
				p.ByType["constraint"], p.ByType["state"], p.Sessions, byteSize(p.RawBytes), note)
		}
		if hidden > 0 {
			fmt.Fprintf(w, "%-22s %6d %4s %4s %4s %4s %5s %8s  %s\n",
				fmt.Sprintf("path-* × %d", hidden), hidClaims, "—", "—", "—", "—", "—",
				byteSize(hidRaw), "no git origin")
		}
		if len(r.Notes) > 0 {
			fmt.Fprintln(w)
			for _, n := range r.Notes {
				fmt.Fprintf(w, "note  %s\n", n)
			}
		}
		if r.Extract != nil {
			formatExtract(w, r.Extract)
		}
		return
	}
	d := r.Detail
	fmt.Fprintf(w, "\nproject %s\n", d.Key)
	fmt.Fprintf(w, "  types  failed=%d decision=%d constraint=%d state=%d\n",
		d.ByType["failed"], d.ByType["decision"], d.ByType["constraint"], d.ByType["state"])
	if len(d.LastAsks) > 0 {
		fmt.Fprintf(w, "  ask    last %s\n", d.LastAsks[0].At)
	} else if r.Records > 0 {
		fmt.Fprintf(w, "  ask    none yet; tape has %d claims\n", r.Records)
	} else {
		fmt.Fprintln(w, "  ask    no tape yet")
	}
	if len(d.Sessions) == 0 {
		fmt.Fprintln(w, "  sessions  (none recorded)")
	}
	for _, s := range d.Sessions {
		cur := "no-cursor"
		for _, c := range r.Cursors {
			if c.Path == s.JSONL {
				cur = fmt.Sprintf("%s %s/%s", c.Status, byteSize(c.Cursor), byteSize(c.Size))
				break
			}
		}
		fmt.Fprintf(w, "  session  %s  %s  %s  %s\n", s.Harness, s.SessionID, filepath.Base(s.JSONL), cur)
	}
	if len(d.Recent) > 0 {
		fmt.Fprintf(w, "\nrecent claims  %d stored, %d ask-would-drop\n", len(d.Recent), d.RecentNoise)
		for _, c := range d.Recent {
			tag := "     "
			if retrieve.ExtractNoise(c) {
				tag = "noise"
			}
			cite := "no-page"
			if d.RecentPage[c.ID] {
				cite = "page"
			}
			fmt.Fprintf(w, "  [%s] %s %s  %s  %s\n", c.Type, tag, c.ID, cite, clip(c.Text, 72))
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
		if len(r.Ask.Dropped) > 0 {
			fmt.Fprintln(w, "  dropped")
			for _, h := range r.Ask.Dropped {
				fmt.Fprintf(w, "    [%s] %s\n      %s\n", h.Type, h.Why, clip(h.Text, 100))
			}
		}
	}
	if r.Extract != nil {
		formatExtract(w, r.Extract)
	}
}

func formatExtract(w io.Writer, e *ExtractView) {
	fmt.Fprintf(w, "\nextract  %s\n", e.JSONL)
	if e.Note != "" {
		fmt.Fprintf(w, "  %s\n", e.Note)
	}
	fmt.Fprintf(w, "  messages %d  sentences %d  drafts %d  kept %d\n",
		e.Messages, e.Sentences, e.Drafts, e.Kept)
	if len(e.SkipCounts) > 0 {
		keys := make([]string, 0, len(e.SkipCounts))
		for k := range e.SkipCounts {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		var bits []string
		for _, k := range keys {
			bits = append(bits, fmt.Sprintf("%s=%d", k, e.SkipCounts[k]))
		}
		fmt.Fprintf(w, "  skip  %s\n", strings.Join(bits, "  "))
	}
	for _, c := range e.Claims {
		fmt.Fprintf(w, "  keep [%s] %s\n", c.Type, clip(c.Text, 88))
	}
	if len(e.Samples) > 0 {
		fmt.Fprintln(w, "  skip samples")
		for _, s := range e.Samples {
			fmt.Fprintf(w, "    [%s] %s\n", s.Reason, clip(s.Text, 88))
		}
	}
}

func healthNotes(stats []store.ProjectStats, curs []store.CursorRow) []string {
	var out []string
	tiny := 0
	for _, p := range stats {
		if tinyPathHash(p) {
			tiny++
		}
	}
	if tiny > 0 {
		out = append(out, fmt.Sprintf("%d path-hash projects with almost no claims (folders without git origin)", tiny))
	}
	nPast, nBehind := 0, 0
	for _, c := range curs {
		switch c.Status {
		case "past-eof":
			nPast++
		case "behind":
			nBehind++
		}
	}
	if nPast+nBehind > 0 {
		out = append(out, fmt.Sprintf("cursors: %d past-eof  %d behind  (catch-up not even with the tape)", nPast, nBehind))
	}
	for _, p := range stats {
		if strings.HasPrefix(p.Key, "path-") || p.Active == 0 || p.RawBytes < 10<<20 {
			continue
		}
		if p.RawBytes/int64(p.Active) >= 500<<10 {
			out = append(out, fmt.Sprintf("%s: %s tape / %d claims", p.Key, byteSize(p.RawBytes), p.Active))
		}
	}
	return out
}

func cursorNotes(sess []store.Session, curs []store.CursorRow) map[string]string {
	jsonlToKey := map[string]string{}
	for _, s := range sess {
		if s.Project != "" {
			jsonlToKey[s.JSONL] = s.Project
		}
	}
	type tally struct{ ok, behind, past, miss int }
	by := map[string]*tally{}
	for _, c := range curs {
		k := jsonlToKey[c.Path]
		if k == "" {
			continue
		}
		t := by[k]
		if t == nil {
			t = &tally{}
			by[k] = t
		}
		switch c.Status {
		case "ok":
			t.ok++
		case "behind":
			t.behind++
		case "past-eof":
			t.past++
		case "missing":
			t.miss++
		}
	}
	out := map[string]string{}
	for k, t := range by {
		if t.behind+t.past+t.miss == 0 {
			out[k] = fmt.Sprintf("%d ok", t.ok)
			continue
		}
		var bits []string
		if t.behind > 0 {
			bits = append(bits, fmt.Sprintf("%d behind", t.behind))
		}
		if t.past > 0 {
			bits = append(bits, fmt.Sprintf("%d past-eof", t.past))
		}
		if t.miss > 0 {
			bits = append(bits, fmt.Sprintf("%d missing", t.miss))
		}
		if t.ok > 0 {
			bits = append(bits, fmt.Sprintf("%d ok", t.ok))
		}
		out[k] = strings.Join(bits, " ")
	}
	return out
}

func tinyPathHash(p store.ProjectStats) bool {
	return strings.HasPrefix(p.Key, "path-") && p.Active <= 1 && p.RawBytes < 100<<10
}

func byteSize(n int64) string {
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
