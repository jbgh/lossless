package embed

import (
	"math"
	"testing"
)

func TestDocumentAndCodec(t *testing.T) {
	if Document("  hello  ", []string{"jose", ""}) != "hello jose" {
		t.Fatal(Document("  hello  ", []string{"jose", ""}))
	}
	v := []float32{3, 4}
	Normalize(v)
	if math.Abs(float64(v[0]-0.6)) > 1e-5 || math.Abs(float64(v[1]-0.8)) > 1e-5 {
		t.Fatalf("%v", v)
	}
	got := Decode(Encode(v))
	if len(got) != 2 || got[0] != v[0] {
		t.Fatalf("%v", got)
	}
	if Cosine(v, v) < 0.99 {
		t.Fatal(Cosine(v, v))
	}
	if Cosine(v, []float32{-v[0], -v[1]}) != 0 {
		t.Fatal("neg")
	}
	if Decode([]byte{1}) != nil {
		t.Fatal("short")
	}
}

func TestClusterParaphraseNearUnrelatedFar(t *testing.T) {
	e := NewCluster(
		[]string{"throttl", "token bucket", "cache idea"},
		[]string{"warehouse", "invoice"},
	)
	vecs, err := e.Embed([]string{
		"add throttling to the API",
		"Redis token bucket failed in staging; connection pool exhausted.",
		"the cache idea we tried",
		"Warehouse query failed because the cursor timed out.",
	})
	if err != nil || len(vecs) != 4 {
		t.Fatal(err, len(vecs))
	}
	near := Cosine(vecs[0], vecs[1])
	also := Cosine(vecs[0], vecs[2])
	far := Cosine(vecs[0], vecs[3])
	if near < 0.55 {
		t.Fatalf("throttling vs token bucket cosine=%v", near)
	}
	if also < 0.55 {
		t.Fatalf("throttling vs cache idea cosine=%v", also)
	}
	if far >= near {
		t.Fatalf("warehouse should be farther: near=%v far=%v", near, far)
	}
}

func TestOpenMissingIsNil(t *testing.T) {
	t.Setenv("LOSSLESS_EMBED_CMD", "")
	t.Setenv("LOSSLESS_EMBED_MODEL", "")
	if Open(t.TempDir()) != nil {
		t.Fatal("expected nil")
	}
}

func TestCmdEmbedderProtocol(t *testing.T) {
	e, err := openCmd(`printf '{"vectors":[[1,0],[0,1]],"model":"toy","dim":2}'`)
	if err != nil {
		t.Fatal(err)
	}
	got, err := e.Embed([]string{"a", "b"})
	if err != nil || len(got) != 2 {
		t.Fatal(err, got)
	}
	if e.Name() != "toy" || e.Dim() != 2 {
		t.Fatalf("%s %d", e.Name(), e.Dim())
	}
	if Cosine(got[0], got[1]) > 0.1 {
		t.Fatalf("expected orthogonal after normalize: %v %v", got[0], got[1])
	}
}
