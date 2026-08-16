package embed

import (
	"hash/fnv"
	"strings"
)

// Cluster is a test/dev embedder: bag-of-tokens plus planted synonym
// groups. Production retrieve uses MiniLM (or a command that speaks it).
// Do not enable this as the default on-box model — it is a synonym list.
type Cluster struct {
	Groups [][]string
	dim    int
}

func NewCluster(groups ...[]string) *Cluster {
	return &Cluster{Groups: groups, dim: 32}
}

func (c *Cluster) Name() string { return "cluster-test" }
func (c *Cluster) Dim() int {
	if c != nil && c.dim > 0 {
		return c.dim
	}
	return 32
}

func (c *Cluster) Embed(texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i, t := range texts {
		out[i] = c.one(t)
	}
	return out, nil
}

func (c *Cluster) one(text string) []float32 {
	dim := c.Dim()
	v := make([]float32, dim)
	low := strings.ToLower(text)
	// Deterministic token hash so unrelated claims sit in different places.
	for _, tok := range strings.FieldsFunc(low, func(r rune) bool {
		return r == ' ' || r == '\n' || r == '\t' || r == ',' || r == '.' || r == ';' || r == ':'
	}) {
		if tok == "" {
			continue
		}
		h := fnv.New32a()
		_, _ = h.Write([]byte(tok))
		v[int(h.Sum32())%dim] += 1
	}
	for gi, g := range c.Groups {
		hit := false
		for _, key := range g {
			if key != "" && strings.Contains(low, strings.ToLower(key)) {
				hit = true
				break
			}
		}
		if !hit {
			continue
		}
		// Extra mass on a reserved cluster axis so paraphrases collapse.
		v[gi%dim] += 8
	}
	Normalize(v)
	return v
}
