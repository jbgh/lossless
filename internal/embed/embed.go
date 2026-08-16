// Package embed is the optional on-box claim embedder.
//
// Missing embedder is degraded retrieve, not a hard failure. Cosine is a
// candidate source and a feature. Path, type, and overlap still outrank it.
package embed

import (
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"strings"
)

// DefaultModel is the on-box sentence model we store in index meta.
const DefaultModel = "all-MiniLM-L6-v2"

// DefaultDim is MiniLM output size.
const DefaultDim = 384

type Embedder interface {
	Name() string
	Dim() int
	Embed(texts []string) ([][]float32, error)
}

// Document is what we embed for a claim: text plus symbols, not the raw tape.
func Document(text string, symbols []string) string {
	var b strings.Builder
	b.WriteString(strings.TrimSpace(text))
	for _, s := range symbols {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		b.WriteByte(' ')
		b.WriteString(s)
	}
	return b.String()
}

func Query(tokens []string) string {
	return strings.Join(tokens, " ")
}

func Cosine(a, b []float32) float32 {
	if len(a) == 0 || len(a) != len(b) {
		return 0
	}
	var dot, na, nb float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		na += float64(a[i]) * float64(a[i])
		nb += float64(b[i]) * float64(b[i])
	}
	if na == 0 || nb == 0 {
		return 0
	}
	c := dot / (math.Sqrt(na) * math.Sqrt(nb))
	if c < 0 {
		return 0
	}
	if c > 1 {
		return 1
	}
	return float32(c)
}

func Normalize(v []float32) {
	var n float64
	for _, x := range v {
		n += float64(x) * float64(x)
	}
	if n == 0 {
		return
	}
	inv := float32(1 / math.Sqrt(n))
	for i := range v {
		v[i] *= inv
	}
}

func Encode(v []float32) []byte {
	b := make([]byte, 4*len(v))
	for i, x := range v {
		binary.LittleEndian.PutUint32(b[i*4:], math.Float32bits(x))
	}
	return b
}

func Decode(b []byte) []float32 {
	if len(b) < 4 || len(b)%4 != 0 {
		return nil
	}
	v := make([]float32, len(b)/4)
	for i := range v {
		v[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[i*4:]))
	}
	return v
}

// Open returns an embedder if one is configured and loadable.
// Never downloads. Never fails the process: missing model is nil, nil.
func Open(home string) Embedder {
	if cmd := strings.TrimSpace(os.Getenv("LOSSLESS_EMBED_CMD")); cmd != "" {
		if e, err := openCmd(cmd); err == nil {
			return e
		}
	}
	if p := strings.TrimSpace(os.Getenv("LOSSLESS_EMBED_MODEL")); p != "" {
		if e, err := openModelDir(p); err == nil {
			return e
		}
	}
	if home == "" {
		return nil
	}
	if e, err := openModelDir(filepath.Join(home, "models", DefaultModel)); err == nil {
		return e
	}
	return nil
}
