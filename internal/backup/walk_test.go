// internal/backup/walk_test.go
package backup

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"lossless/internal/backup/crypt"
	"lossless/internal/claim"
	"lossless/internal/store"
	"lossless/internal/write"
)

func testKeys(t *testing.T) *crypt.Keys {
	t.Helper()
	h, _ := crypt.NewKeyHex()
	k, err := crypt.ParseKeyHex(h)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

// seedHome builds a store with two claims, one sealed part, one live part,
// and every kind of file the walk must skip.
func seedHome(t *testing.T) (string, *store.Store) {
	t.Helper()
	home := t.TempDir()
	st, err := store.Open(home)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	for _, txt := range []string{"Use jose, not jsonwebtoken, for Edge.", "Redis limiter blew the p95 budget."} {
		if _, err := st.WriteClaim(claim.Record{Type: "decision", Text: txt, ProjectKey: "acme/api"}); err != nil {
			t.Fatal(err)
		}
	}
	rawDir := filepath.Join(home, "raw", "acme__api", "2026-09")
	must(t, os.MkdirAll(rawDir, 0o700))
	must(t, os.WriteFile(filepath.Join(rawDir, "sealed.jsonl"), []byte(`{"role":"user","content":"sealed"}`+"\n"), 0o600))
	if _, err := write.SealRaw(filepath.Join(rawDir, "sealed.jsonl")); err != nil {
		t.Fatal(err)
	}
	must(t, os.WriteFile(filepath.Join(rawDir, "live.jsonl"), []byte(`{"role":"user","content":"live"}`+"\n"), 0o600))
	must(t, os.WriteFile(filepath.Join(rawDir, "live.jsonl.lock"), nil, 0o600))
	must(t, os.WriteFile(filepath.Join(rawDir, "junk.jsonl.zst.tmp"), []byte("x"), 0o600))
	must(t, os.WriteFile(filepath.Join(home, "export", "acme__api", "stale.md.tmp"), []byte("x"), 0o600))
	must(t, os.Symlink(filepath.Join(rawDir, "live.jsonl"), filepath.Join(home, "export", "acme__api", "link.md")))
	must(t, os.WriteFile(filepath.Join(home, "index", "excerpts-2026-07.sqlite"), nil, 0o600))
	must(t, copyFile(filepath.Join(home, "index", "claims.sqlite"), filepath.Join(home, "index", "excerpts-2026-08.sqlite")))
	must(t, os.WriteFile(filepath.Join(home, "service.env"), []byte("LOSSLESS_HOME=x\n"), 0o600))
	must(t, os.WriteFile(filepath.Join(home, "spool", "push-1.json"), []byte("{}"), 0o600))
	return home, st
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func copyFile(src, dst string) error {
	b, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, b, 0o600)
}

func rels(items []Item) []string {
	var out []string
	for _, it := range items {
		out = append(out, it.Rel)
	}
	sort.Strings(out)
	return out
}

func TestWalkSelectsExactlyTheStore(t *testing.T) {
	home, _ := seedHome(t)
	tmp := filepath.Join(home, "backup-tmp")
	items, err := walk(home, tmp, testKeys(t), LoadState(home))
	if err != nil {
		t.Fatal(err)
	}
	got := rels(items)
	var want []string
	for _, r := range []string{"raw/acme__api/2026-09/sealed.jsonl.zst", "raw/acme__api/2026-09/live.jsonl", "index/claims.sqlite", "index/excerpts-2026-08.sqlite"} {
		want = append(want, r)
	}
	mds, _ := filepath.Glob(filepath.Join(home, "export", "acme__api", "*.md"))
	for _, m := range mds {
		if strings.HasSuffix(m, "link.md") {
			continue
		}
		want = append(want, "export/acme__api/"+filepath.Base(m))
	}
	sort.Strings(want)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("\n got %v\nwant %v", got, want)
	}
	for _, it := range items {
		if it.SHA256 == "" || !strings.HasPrefix(it.Object, "o/") || !strings.HasSuffix(it.Object, "/"+it.SHA256) {
			t.Fatalf("item %+v", it)
		}
		switch {
		case strings.HasSuffix(it.Rel, "live.jsonl"), strings.HasSuffix(it.Rel, ".sqlite"):
			if !it.Temp || !strings.HasPrefix(it.Src, tmp) {
				t.Fatalf("live and sqlite must be temp copies: %+v", it)
			}
			b, _ := os.ReadFile(it.Src)
			if strings.HasSuffix(it.Rel, "live.jsonl") && string(b) != `{"role":"user","content":"live"}`+"\n" {
				t.Fatalf("live copy content: %q", b)
			}
		default:
			if it.Temp || !strings.HasPrefix(it.Src, home) {
				t.Fatalf("direct read expected: %+v", it)
			}
		}
	}
}

func TestWalkReusesCachedHashWhenStatUnchanged(t *testing.T) {
	home, _ := seedHome(t)
	tmp := filepath.Join(home, "backup-tmp")
	k := testKeys(t)
	cache := LoadState(home)
	items, err := walk(home, tmp, k, cache)
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range items {
		cache.Files[it.Rel] = FileState{Size: it.Size, StatSize: it.StatSize, Mtime: it.Mtime, SHA256: it.SHA256, Object: it.Object}
	}
	sealed := filepath.Join(home, "raw", "acme__api", "2026-09", "sealed.jsonl.zst")
	st, _ := os.Stat(sealed)
	b, _ := os.ReadFile(sealed)
	b[len(b)-1] ^= 0xff // same size, different bytes
	must(t, os.WriteFile(sealed, b, 0o600))
	must(t, os.Chtimes(sealed, st.ModTime(), st.ModTime()))
	again, err := walk(home, tmp, k, cache)
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range again {
		if it.Rel == "raw/acme__api/2026-09/sealed.jsonl.zst" && it.SHA256 != cache.Files[it.Rel].SHA256 {
			t.Fatal("unchanged size+mtime must reuse the cached hash")
		}
	}
	must(t, os.Chtimes(sealed, time.Now(), time.Now()))
	third, _ := walk(home, tmp, k, cache)
	for _, it := range third {
		if it.Rel == "raw/acme__api/2026-09/sealed.jsonl.zst" && it.SHA256 == cache.Files[it.Rel].SHA256 {
			t.Fatal("a new mtime must re-hash")
		}
	}
}

func TestLiveItemFallsBackToSealedSibling(t *testing.T) {
	home, _ := seedHome(t)
	tmp := filepath.Join(home, "backup-tmp")
	must(t, os.MkdirAll(tmp, 0o700))
	k := testKeys(t)
	// The walk saw live.jsonl, then the watcher sealed it before the copy.
	live := filepath.Join(home, "raw", "acme__api", "2026-09", "live.jsonl")
	if _, err := write.SealRaw(live); err != nil {
		t.Fatal(err)
	}
	it, err := liveItem(home, tmp, "raw/acme__api/2026-09/live.jsonl", k, LoadState(home))
	if err != nil || it.Rel != "raw/acme__api/2026-09/live.jsonl.zst" || it.Temp {
		t.Fatalf("%+v %v", it, err)
	}
}
