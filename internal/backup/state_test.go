package backup

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestStateRoundTrip(t *testing.T) {
	home := t.TempDir()
	s := LoadState(home)
	if len(s.Files) != 0 || s.LastOK != "" {
		t.Fatalf("fresh state: %+v", s)
	}
	sha := strings.Repeat("ab", 32)
	s.Files["raw/a.jsonl.zst"] = FileState{Size: 3, Mtime: 4, SHA256: sha, Object: "o/n/" + sha}
	s.LastOK = time.Date(2026, 9, 14, 1, 2, 3, 0, time.UTC).Format(time.RFC3339)
	s.Adopt("c-1")
	s.Adopt("c-1")
	s.Manifests["g1"] = &Manifest{Generation: "g1", Files: map[string]FileEntry{"x": {SHA256: "s", Size: 1, Object: "o"}}}
	if err := s.Save(home); err != nil {
		t.Fatal(err)
	}
	st, _ := os.Stat(filepath.Join(home, "backup-state.json"))
	if st.Mode().Perm() != 0o600 {
		t.Fatal(st.Mode())
	}
	got := LoadState(home)
	if got.Files["raw/a.jsonl.zst"].SHA256 != sha || len(got.Adopted) != 1 || got.Manifests["g1"].Files["x"].Size != 1 {
		t.Fatalf("%+v", got)
	}
	ok, has := got.LastOKTime()
	if !has || ok.Hour() != 1 {
		t.Fatal(ok, has)
	}
	if !got.AllowsWriter("me", "") || !got.AllowsWriter("me", "me") || !got.AllowsWriter("me", "c-1") || got.AllowsWriter("me", "c-2") {
		t.Fatal("AllowsWriter")
	}
}

func TestStateCorruptFileIsEmpty(t *testing.T) {
	home := t.TempDir()
	_ = os.WriteFile(filepath.Join(home, "backup-state.json"), []byte("{not json"), 0o600)
	if s := LoadState(home); len(s.Files) != 0 || s.Files == nil {
		t.Fatalf("%+v", s)
	}
}

func TestNewGenerationShape(t *testing.T) {
	g := NewGeneration(time.Date(2026, 9, 12, 20, 15, 0, 0, time.UTC))
	if len(g) != len("20260912T201500Z-a1f3") || g[:16] != "20260912T201500Z" || g[16] != '-' {
		t.Fatal(g)
	}
	if NewGeneration(time.Now()) == NewGeneration(time.Now()) {
		t.Fatal("random suffix must differ")
	}
}

func TestStateDropsMalformedCachedHash(t *testing.T) {
	home := t.TempDir()
	good := strings.Repeat("0", 64)
	_ = os.WriteFile(filepath.Join(home, "backup-state.json"), []byte(`{"files":{"a":{"size":1,"mtime_ns":1,"sha256":"abc","object":"o/a/abc"},"b":{"size":1,"mtime_ns":1,"sha256":"`+good+`","object":"o/b/`+good+`"}}}`), 0o600)
	s := LoadState(home)
	if _, ok := s.Files["a"]; ok {
		t.Fatal("a short sha256 must not survive load; uploadOne slices it")
	}
	if _, ok := s.Files["b"]; !ok {
		t.Fatal("a well-formed entry must survive")
	}
}
