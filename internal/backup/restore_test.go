// internal/backup/restore_test.go
package backup

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"lossless/internal/backup/s3"
	"lossless/internal/backup/s3/s3test"
	"lossless/internal/store"
)

func noDaemon(string) bool { return false }

// freshTarget imitates a new machine after `lossless setup`: an empty store
// whose index exists, plus the copied backup.env and backup.key.
func freshTarget(t *testing.T, from string) string {
	t.Helper()
	home := t.TempDir()
	st, err := store.Open(home)
	must(t, err)
	must(t, st.Close())
	must(t, copyFile(filepath.Join(from, "backup.env"), filepath.Join(home, "backup.env")))
	must(t, copyFile(filepath.Join(from, "backup.key"), filepath.Join(home, "backup.key")))
	return home
}

func claimTexts(t *testing.T, home string) []string {
	t.Helper()
	st, err := store.Open(home)
	must(t, err)
	defer st.Close()
	active, err := st.ListActive("acme/api")
	must(t, err)
	var out []string
	for _, c := range active {
		out = append(out, c.Text)
	}
	sort.Strings(out)
	return out
}

func TestRestoreIntoFreshStoreThenBackupJustWorks(t *testing.T) {
	srv := s3test.New()
	defer srv.Close()
	src := setupBackup(t, srv, 5)
	first := runOK(t, src, RunOptions{})
	dst := freshTarget(t, src)
	sum, err := Restore(context.Background(), dst, RestoreOptions{Health: noDaemon})
	if err != nil {
		t.Fatal(err)
	}
	if sum.Generation != first.Generation || sum.Restored != first.Uploaded || sum.LeftAlone != 0 {
		t.Fatalf("%+v", sum)
	}
	if strings.Join(claimTexts(t, dst), "|") != strings.Join(claimTexts(t, src), "|") {
		t.Fatal("claims differ after restore")
	}
	a, _ := os.ReadFile(filepath.Join(src, "raw", "acme__api", "2026-09", "sealed.jsonl.zst"))
	b, _ := os.ReadFile(filepath.Join(dst, "raw", "acme__api", "2026-09", "sealed.jsonl.zst"))
	if string(a) != string(b) {
		t.Fatal("sealed part differs")
	}
	for _, rel := range []string{"raw/acme__api/2026-09/live.jsonl", "index/claims.sqlite"} {
		st, err := os.Stat(filepath.Join(dst, filepath.FromSlash(rel)))
		if err != nil || st.Mode().Perm() != 0o600 {
			t.Fatalf("%s: %v %v", rel, err, st)
		}
	}
	if _, err := os.Stat(filepath.Join(dst, "index", "claims.sqlite-wal")); !os.IsNotExist(err) {
		t.Fatal("a stale WAL next to the restored index must be removed")
	}
	state := LoadState(dst)
	if state.LastOK != "" || len(state.Files) != first.Uploaded || len(state.Adopted) != 1 {
		t.Fatalf("state %+v", state)
	}
	// The new machine is now the writer without --take-over.
	again := runOK(t, dst, RunOptions{})
	if !again.NoChange {
		t.Fatalf("restored store must be in sync: %+v", again)
	}
	// Restoring again skips everything.
	sum2, err := Restore(context.Background(), dst, RestoreOptions{Health: noDaemon, Force: true})
	if err != nil || sum2.Restored != 0 || sum2.Skipped != first.Uploaded {
		t.Fatalf("%+v %v", sum2, err)
	}
}

func TestRestoreGuards(t *testing.T) {
	srv := s3test.New()
	defer srv.Close()
	src := setupBackup(t, srv, 5)
	runOK(t, src, RunOptions{})
	dst := freshTarget(t, src)
	if _, err := Restore(context.Background(), dst, RestoreOptions{Health: func(string) bool { return true }}); !errors.Is(err, ErrDaemonRunning) {
		t.Fatalf("want ErrDaemonRunning, got %v", err)
	}
	// A seeded store is not empty.
	busy, _ := seedHome(t)
	must(t, copyFile(filepath.Join(src, "backup.env"), filepath.Join(busy, "backup.env")))
	must(t, copyFile(filepath.Join(src, "backup.key"), filepath.Join(busy, "backup.key")))
	if _, err := Restore(context.Background(), busy, RestoreOptions{Health: noDaemon}); !errors.Is(err, ErrStoreNotEmpty) {
		t.Fatalf("want ErrStoreNotEmpty, got %v", err)
	}
	touchLive(t, busy, `{"role":"user","content":"local only"}`) // now differs from the bucket
	sum, err := Restore(context.Background(), busy, RestoreOptions{Health: noDaemon, Force: true})
	if err != nil {
		t.Fatal(err)
	}
	// busy has its own claim files (different ids) and its own live part:
	// those are left alone; the index snapshots are replaced.
	if sum.LeftAlone != 1 || strings.Join(sum.Left, ",") != "raw/acme__api/2026-09/live.jsonl" {
		t.Fatalf("%+v", sum)
	}
	for _, l := range sum.Left {
		if strings.HasPrefix(l, "index/") {
			t.Fatalf("index must be overwritten under --force: %v", sum.Left)
		}
	}
	// Sidecar off: warning, no refusal.
	t.Setenv("LOSSLESS_SIDECAR", "off")
	var out strings.Builder
	fresh := freshTarget(t, src)
	if _, err := Restore(context.Background(), fresh, RestoreOptions{Out: &out}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "stop lossless serve") {
		t.Fatalf("expected a warning, got %q", out.String())
	}
}

func TestRestoreAtAndList(t *testing.T) {
	srv := s3test.New()
	defer srv.Close()
	src := setupBackup(t, srv, 5)
	g1 := runOK(t, src, RunOptions{}).Generation
	touchLive(t, src, `{"role":"assistant","content":"second"}`)
	g2 := runOK(t, src, RunOptions{}).Generation
	gens, err := List(context.Background(), src)
	if err != nil || len(gens) != 2 || gens[0].Generation != g2 || !gens[0].Latest || gens[1].Generation != g1 || gens[1].Latest {
		t.Fatalf("%+v %v", gens, err)
	}
	if gens[0].Files == 0 || gens[0].Bytes == 0 || gens[0].Lossless == "" {
		t.Fatalf("%+v", gens[0])
	}
	dst := freshTarget(t, src)
	sum, err := Restore(context.Background(), dst, RestoreOptions{Health: noDaemon, At: g1})
	if err != nil || sum.Generation != g1 {
		t.Fatalf("%+v %v", sum, err)
	}
	b, _ := os.ReadFile(filepath.Join(dst, "raw", "acme__api", "2026-09", "live.jsonl"))
	if strings.Contains(string(b), "second") {
		t.Fatal("--at g1 must restore the older live part")
	}
	if _, err := Restore(context.Background(), freshTarget(t, src), RestoreOptions{Health: noDaemon, At: "nope"}); err == nil || !strings.Contains(err.Error(), g2) {
		t.Fatalf("unknown generation must list the kept ones: %v", err)
	}
}

// TestRestoreRejectsBadManifestPath is the regression test for finding 3:
// restore must reject a manifest entry whose rel escapes home, rather than
// trusting filepath.Join(home, rel) to stay inside it. The malicious entry
// points at a real, validly encrypted object (bound to the bad rel as AAD,
// with a matching plaintext hash) so that, absent the guard, Get, Decrypt,
// and Rename would all succeed and actually write outside home.
func TestRestoreRejectsBadManifestPath(t *testing.T) {
	srv := s3test.New()
	defer srv.Close()
	home := setupBackup(t, srv, 5)

	keys, err := LoadKey(home)
	must(t, err)
	c, err := s3.New(srv.Config("bkt", "pre"))
	must(t, err)
	r := &remote{c: c, keys: keys}

	badRel := "raw/../../.ssh/authorized_keys"
	plaintext := []byte("evil\n")
	var ct bytes.Buffer
	plainSHA, cipherSHA, n, err := r.keys.Encrypt(&ct, bytes.NewReader(plaintext), badRel)
	must(t, err)
	objKey := objectKey(r.keys.Name(badRel), plainSHA)
	must(t, r.c.Put(context.Background(), objKey, bytes.NewReader(ct.Bytes()), n, cipherSHA))

	gen := NewGeneration(time.Now())
	m := &Manifest{
		Version:     manifestVersion,
		Generation:  gen,
		CreatedAt:   time.Now().UTC().Format(time.RFC3339),
		Client:      "x",
		Generations: []string{gen},
		Files: map[string]FileEntry{
			badRel: {SHA256: plainSHA, Size: int64(len(plaintext)), Object: objKey},
		},
	}
	must(t, r.putManifest(context.Background(), pointerKey, m))

	dst := freshTarget(t, home)
	if _, err := Restore(context.Background(), dst, RestoreOptions{Health: noDaemon}); err == nil || !strings.Contains(err.Error(), badRel) {
		t.Fatalf("want an error naming the bad rel, got %v", err)
	}
	escaped := filepath.Join(dst, filepath.FromSlash(badRel))
	if _, err := os.Stat(escaped); !os.IsNotExist(err) {
		t.Fatal("must not write outside home")
	}
}

func TestRestoreNothingToRestore(t *testing.T) {
	srv := s3test.New()
	defer srv.Close()
	src := setupBackup(t, srv, 5)
	if _, err := Restore(context.Background(), freshTarget(t, src), RestoreOptions{Health: noDaemon}); !errors.Is(err, ErrNothingToRestore) {
		t.Fatalf("want ErrNothingToRestore, got %v", err)
	}
}
