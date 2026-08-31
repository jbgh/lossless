// Package bench is the lossless memory benchmark: ingest simulated
// harness sessions, then score whether ask() packs the gold context.
package bench

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"lossless/internal/claim"
	"lossless/internal/retrieve"
	"lossless/internal/store"
	"lossless/internal/write"
)

type SessionIngest struct {
	Harness   string `json:"harness"`
	SessionID string `json:"session_id"`
	File      string `json:"file"`
	Source    string `json:"source,omitempty"`
}

type SeedClaim struct {
	Type      string   `json:"type"`
	Text      string   `json:"text"`
	Paths     []string `json:"paths"`
	CreatedAt string   `json:"created_at"`
	Harness   string   `json:"harness,omitempty"`
}

type WriteExpect struct {
	MinExtracted             int      `json:"min_extracted"`
	MustExtractTypes         []string `json:"must_extract_types"`
	MustExtractSubstrings    []string `json:"must_extract_substrings"`
	MustNotExtractSubstrings []string `json:"must_not_extract_substrings"`
}

type AskExpect struct {
	Name                     string           `json:"name"`
	Request                  retrieve.Request `json:"request"`
	MustIncludeTypes         []string         `json:"must_include_types"`
	MustIncludeSubstrings    []string         `json:"must_include_substrings"`
	MustNotIncludeSubstrings []string         `json:"must_not_include_substrings"`
	MustWarn                 string           `json:"must_warn"`
	MustNotWarn              bool             `json:"must_not_warn"`
	MustBeEmpty              bool             `json:"must_be_empty"`
	FirstMustNotContain      string           `json:"first_must_not_contain"`
}

type Case struct {
	ID       string         `json:"id"`
	Project  string         `json:"project"`
	Now      string         `json:"now,omitempty"`
	Sessions []SessionIngest `json:"sessions"`
	Seed     []SeedClaim    `json:"seed,omitempty"`
	Write    WriteExpect    `json:"write"`
	Asks     []AskExpect    `json:"asks"`
}

type AskScore struct {
	Name    string            `json:"name"`
	Needles int               `json:"needles"`
	Hits    int               `json:"hits"`
	Recall  float64           `json:"recall"`
	Errors  []string          `json:"errors,omitempty"`
	Packet  retrieve.Response `json:"packet"`
}

type CaseScore struct {
	ID           string     `json:"id"`
	WriteOK      bool       `json:"write_ok"`
	WriteErrors  []string   `json:"write_errors,omitempty"`
	Extracted    int        `json:"extracted"`
	Asks         []AskScore `json:"asks"`
	AskPass      int        `json:"ask_pass"`
	AskTotal     int        `json:"ask_total"`
	Recall       float64    `json:"recall"`
}

type Report struct {
	Cases      []CaseScore `json:"cases"`
	CasePass   int         `json:"case_pass"`
	CaseTotal  int         `json:"case_total"`
	AskPass    int         `json:"ask_pass"`
	AskTotal   int         `json:"ask_total"`
	MeanRecall float64     `json:"mean_recall"`
}

func LoadCases(root string) ([]Case, error) {
	files, err := filepath.Glob(filepath.Join(root, "cases", "*.json"))
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	var out []Case
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			return nil, err
		}
		var c Case
		if err := json.Unmarshal(b, &c); err != nil {
			return nil, fmt.Errorf("%s: %w", f, err)
		}
		if c.ID == "" {
			c.ID = strings.TrimSuffix(filepath.Base(f), ".json")
		}
		out = append(out, c)
	}
	return out, nil
}

func RunDir(root, home string) (Report, error) {
	cases, err := LoadCases(root)
	if err != nil {
		return Report{}, err
	}
	var rep Report
	for _, c := range cases {
		dir := home
		if home != "" {
			dir = filepath.Join(home, c.ID)
		} else {
			var err error
			// The home name matches the isolated-test-store pattern so
			// refuseTestIngest admits fixture sessions here, exactly as
			// it does under go test, while live homes keep refusing them.
			// The literal 000 suffix guarantees the trailing digits the
			// pattern requires even when the random part is short;
			// TestBenchHomeAdmitsFixtureIngest pins the contract.
			dir, err = os.MkdirTemp("", "TestBench"+alnumOnly(c.ID)+"*000")
			if err != nil {
				return Report{}, err
			}
		}
		sc, err := RunCase(root, dir, c)
		if err != nil {
			return Report{}, fmt.Errorf("%s: %w", c.ID, err)
		}
		rep.Cases = append(rep.Cases, sc)
		rep.CaseTotal++
		if sc.WriteOK && sc.AskPass == sc.AskTotal {
			rep.CasePass++
		}
		rep.AskPass += sc.AskPass
		rep.AskTotal += sc.AskTotal
	}
	// Mean over cases that ask. A write-expectations-only case has no
	// recall to report and must not read as zero.
	askCases := 0
	for _, sc := range rep.Cases {
		if sc.AskTotal > 0 {
			rep.MeanRecall += sc.Recall
			askCases++
		}
	}
	if askCases > 0 {
		rep.MeanRecall /= float64(askCases)
	}
	return rep, nil
}

func alnumOnly(s string) string {
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func RunCase(root, home string, c Case) (CaseScore, error) {
	out := CaseScore{ID: c.ID, AskTotal: len(c.Asks)}
	st, err := store.Open(home)
	if err != nil {
		return out, err
	}
	defer st.Close()

	project := c.Project
	if project == "" {
		project = "acme/api"
	}
	for i, seed := range c.Seed {
		rec := claim.Record{
			Type: seed.Type, Text: seed.Text, Paths: seed.Paths,
			CreatedAt: seed.CreatedAt, ProjectKey: project,
			Harness: seed.Harness, SessionID: "seed", Source: "import", Status: "active",
		}
		if rec.Harness == "" {
			rec.Harness = "grok"
		}
		if _, err := st.WriteClaim(rec); err != nil {
			return out, fmt.Errorf("seed %d: %w", i, err)
		}
	}

	extracted := 0
	for _, sess := range c.Sessions {
		src := sess.Source
		if src == "" {
			src = "compact"
		}
		res, err := write.CatchUp(st, write.CatchUpRequest{
			JSONL: filepath.Join(root, sess.File), Project: project,
			Harness: sess.Harness, SessionID: sess.SessionID, Source: src,
		})
		if err != nil {
			return out, fmt.Errorf("catch-up %s: %w", sess.SessionID, err)
		}
		extracted += res.Extracted
	}
	out.Extracted = extracted

	active, err := st.ListActive(project)
	if err != nil {
		return out, err
	}
	out.WriteErrors = scoreWrite(c.Write, extracted, active)
	out.WriteOK = len(out.WriteErrors) == 0

	now := time.Now().UTC()
	if c.Now != "" {
		if t, err := time.Parse(time.RFC3339, c.Now); err == nil {
			now = t
		}
	}
	eng := retrieve.Engine{Store: st, Now: func() time.Time { return now }}

	var recSum float64
	for _, ask := range c.Asks {
		req := ask.Request
		if req.Project == "" {
			req.Project = project
		}
		pkt, err := eng.Ask(req)
		if err != nil {
			return out, fmt.Errorf("ask %s: %w", ask.Name, err)
		}
		as := scoreAsk(ask, pkt)
		out.Asks = append(out.Asks, as)
		recSum += as.Recall
		if len(as.Errors) == 0 {
			out.AskPass++
		}
	}
	if n := len(c.Asks); n > 0 {
		out.Recall = recSum / float64(n)
	}
	return out, nil
}

func scoreWrite(exp WriteExpect, extracted int, recs []claim.Record) []string {
	var errs []string
	if exp.MinExtracted > 0 && extracted < exp.MinExtracted {
		errs = append(errs, fmt.Sprintf("extracted %d want >= %d", extracted, exp.MinExtracted))
	}
	types := map[string]bool{}
	var blob strings.Builder
	for _, r := range recs {
		types[r.Type] = true
		blob.WriteString(r.Text)
		blob.WriteByte('\n')
	}
	text := blob.String()
	for _, typ := range exp.MustExtractTypes {
		if !types[typ] {
			errs = append(errs, "missing extracted type "+typ)
		}
	}
	for _, s := range exp.MustExtractSubstrings {
		if !strings.Contains(text, s) {
			errs = append(errs, fmt.Sprintf("extract missing %q", s))
		}
	}
	for _, s := range exp.MustNotExtractSubstrings {
		if strings.Contains(text, s) {
			errs = append(errs, fmt.Sprintf("extract leaked %q", s))
		}
	}
	return errs
}

func scoreAsk(exp AskExpect, pkt retrieve.Response) AskScore {
	as := AskScore{Name: exp.Name, Packet: pkt}
	var blob strings.Builder
	types := map[string]bool{}
	for _, h := range pkt.Context {
		blob.WriteString(h.Text)
		blob.WriteByte('\n')
		types[h.Type] = true
	}
	text := blob.String()
	needles := append([]string{}, exp.MustIncludeSubstrings...)
	needles = append(needles, exp.MustIncludeTypes...)
	as.Needles = len(needles)
	if exp.MustBeEmpty {
		as.Needles++
		if len(pkt.Context) == 0 && len(pkt.Warnings) == 0 {
			as.Hits++
		} else {
			as.Errors = append(as.Errors, "want empty packet")
		}
	}
	for _, s := range exp.MustIncludeSubstrings {
		if strings.Contains(text, s) {
			as.Hits++
		} else {
			as.Errors = append(as.Errors, fmt.Sprintf("missing %q", s))
		}
	}
	for _, typ := range exp.MustIncludeTypes {
		if types[typ] {
			as.Hits++
		} else {
			as.Errors = append(as.Errors, "missing type "+typ)
		}
	}
	for _, s := range exp.MustNotIncludeSubstrings {
		if strings.Contains(text, s) {
			as.Errors = append(as.Errors, fmt.Sprintf("unexpected %q", s))
		}
	}
	if exp.FirstMustNotContain != "" && len(pkt.Context) > 0 && strings.Contains(pkt.Context[0].Text, exp.FirstMustNotContain) {
		as.Errors = append(as.Errors, "first hit contains "+exp.FirstMustNotContain)
	}
	if exp.MustWarn != "" {
		as.Needles++
		ok := false
		for _, w := range pkt.Warnings {
			if strings.Contains(strings.ToLower(w), exp.MustWarn) {
				ok = true
			}
		}
		if ok {
			as.Hits++
		} else {
			as.Errors = append(as.Errors, "missing warning "+exp.MustWarn)
		}
	}
	if exp.MustNotWarn && len(pkt.Warnings) != 0 {
		as.Errors = append(as.Errors, fmt.Sprintf("unexpected warnings %v", pkt.Warnings))
	}
	if as.Needles > 0 {
		as.Recall = float64(as.Hits) / float64(as.Needles)
	} else if len(as.Errors) == 0 {
		as.Recall = 1
	}
	return as
}

func FormatReport(rep Report) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%-32s %-6s %8s %7s  notes\n", "CASE", "WRITE", "ASKS", "RECALL")
	for _, c := range rep.Cases {
		w := "ok"
		if !c.WriteOK {
			w = "FAIL"
		}
		notes := strings.Join(c.WriteErrors, "; ")
		for _, a := range c.Asks {
			if len(a.Errors) > 0 {
				if notes != "" {
					notes += "; "
				}
				notes += a.Name + ": " + strings.Join(a.Errors, ", ")
			}
		}
		fmt.Fprintf(&b, "%-32s %-6s %3d/%-3d %6.2f  %s\n", c.ID, w, c.AskPass, c.AskTotal, c.Recall, notes)
	}
	fmt.Fprintf(&b, "\ncases %d/%d  asks %d/%d  mean recall %.2f\n",
		rep.CasePass, rep.CaseTotal, rep.AskPass, rep.AskTotal, rep.MeanRecall)
	return b.String()
}
