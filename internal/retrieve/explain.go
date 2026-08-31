package retrieve

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"lossless/internal/claim"
	"lossless/internal/embed"
	"lossless/internal/store"
)

// Trace is why ask packed (and dropped) the rows it did. Ranking is unchanged.
type Trace struct {
	Project  string     `json:"project"`
	Warnings []string   `json:"warnings,omitempty"`
	Packed   []TraceHit `json:"packed,omitempty"`
	Dropped  []TraceHit `json:"dropped,omitempty"`
}

type TraceHit struct {
	ID      string   `json:"id"`
	Type    string   `json:"type"`
	Text    string   `json:"text"`
	When    string   `json:"when,omitempty"`
	Paths   []string `json:"paths,omitempty"`
	AgeDays float64  `json:"age_days,omitempty"`
	Score   float64  `json:"score,omitempty"`
	Path    float64  `json:"path,omitempty"`
	Symbol  float64  `json:"symbol,omitempty"`
	BM25    float64  `json:"bm25,omitempty"`
	Fail    float64  `json:"fail,omitempty"`
	Shipped float64  `json:"shipped,omitempty"`
	Why     string   `json:"why,omitempty"`
}

// ExtractNoise is true when ask would drop this stored claim as extract residue.
func ExtractNoise(rec claim.Record) bool {
	return extractNoise(rec)
}

func Explain(st *store.Store, req Request) (Trace, error) {
	var emb embed.Embedder
	if st != nil {
		emb = st.Embedder
	}
	return (Engine{Store: st, Embedder: emb}).Explain(req)
}

// Explain runs the same pipeline as Ask and records packed scores plus drop reasons.
func (e Engine) Explain(req Request) (Trace, error) {
	p, err := e.prepare(req)
	if err != nil {
		return Trace{}, err
	}
	now := e.now().UTC()
	tr := Trace{Project: p.out.Project, Warnings: p.out.Warnings}
	for _, c := range p.packed {
		tr.Packed = append(tr.Packed, hitFromScored(c, now, ""))
	}
	sort.SliceStable(p.drops, func(i, j int) bool {
		if p.drops[i].scored != p.drops[j].scored {
			return p.drops[i].scored
		}
		return p.drops[i].sc.score > p.drops[j].sc.score
	})
	const dropCap = 10
	for _, d := range p.drops {
		if len(tr.Dropped) >= dropCap {
			break
		}
		tr.Dropped = append(tr.Dropped, hitFromDrop(d, now))
	}
	return tr, nil
}

func hitFromScored(c scored, now time.Time, drop string) TraceHit {
	h := TraceHit{
		ID: c.rec.ID, Type: c.rec.Type, Text: c.rec.Text, When: c.rec.CreatedAt,
		Paths: c.rec.Paths, Score: c.score, Path: c.path, Symbol: c.symbol,
		BM25: c.bm25, Fail: c.pFail(), Shipped: c.pRegress(),
	}
	if t, err := time.Parse(time.RFC3339, c.rec.CreatedAt); err == nil {
		h.AgeDays = now.Sub(t).Hours() / 24
		if h.AgeDays < 0 {
			h.AgeDays = 0
		}
	}
	h.Why = formatWhy(h, drop)
	return h
}

func hitFromDrop(d traceDrop, now time.Time) TraceHit {
	if d.scored {
		return hitFromScored(d.sc, now, d.reason)
	}
	h := TraceHit{
		ID: d.rec.ID, Type: d.rec.Type, Text: d.rec.Text, When: d.rec.CreatedAt,
		Paths: d.rec.Paths, Why: d.reason,
	}
	if t, err := time.Parse(time.RFC3339, d.rec.CreatedAt); err == nil {
		h.AgeDays = now.Sub(t).Hours() / 24
		if h.AgeDays < 0 {
			h.AgeDays = 0
		}
	}
	return h
}

func formatWhy(h TraceHit, drop string) string {
	var bits []string
	if drop != "" {
		bits = append(bits, drop)
	} else {
		bits = append(bits, h.Type)
	}
	if h.Score != 0 || drop == "" {
		bits = append(bits, fmt.Sprintf("score=%.2f", h.Score))
	}
	if h.Path > 0 {
		bits = append(bits, fmt.Sprintf("path=%.2f", h.Path))
	} else {
		bits = append(bits, "path=0")
	}
	if h.Symbol > 0 {
		bits = append(bits, fmt.Sprintf("sym=%.2f", h.Symbol))
	}
	if h.Fail > 0 {
		bits = append(bits, fmt.Sprintf("fail=%.0f", h.Fail))
	}
	if h.Shipped > 0 {
		bits = append(bits, fmt.Sprintf("ship=%.0f", h.Shipped))
	}
	if h.AgeDays >= 1 {
		bits = append(bits, fmt.Sprintf("age=%.0fd", h.AgeDays))
	}
	return strings.Join(bits, " ")
}
