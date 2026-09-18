// internal/backup/run_test.go
package backup

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"lossless/internal/backup/s3"
	"lossless/internal/backup/s3/s3test"
	"lossless/internal/store"
)

// setupBackup seeds a store, points backup.env at the fake bucket, and
// silences retry sleeps.
func setupBackup(t *testing.T, srv *s3test.Server, keep int) string {
	t.Helper()
	clearBackupEnv(t)
	t.Setenv("LOSSLESS_BACKUP_ACCESS_KEY", "test")
	t.Setenv("LOSSLESS_BACKUP_SECRET_KEY", "secret")
	t.Setenv("LOSSLESS_CLIENT", "")
	home, _ := seedHome(t)
	must(t, Init(home, InitOptions{URL: "s3://bkt/pre", Endpoint: srv.URL(), Every: time.Hour, Keep: keep}))
	sleepHook = func(time.Duration) {}
	t.Cleanup(func() { sleepHook = nil })
	return home
}

func runOK(t *testing.T, home string, o RunOptions) Summary {
	t.Helper()
	sum, err := Run(context.Background(), home, o)
	if err != nil {
		t.Fatal(err)
	}
	return sum
}

func pointerOf(t *testing.T, home string, srv *s3test.Server) *Manifest {
	return pointerAtPrefix(t, home, "pre", srv)
}

// pointerAtPrefix reads the pointer manifest at an arbitrary prefix in the
// same fake bucket, using the given home's backup.key.
func pointerAtPrefix(t *testing.T, home, prefix string, srv *s3test.Server) *Manifest {
	t.Helper()
	keys, err := LoadKey(home)
	must(t, err)
	r := &remote{keys: keys}
	c, err := s3.New(srv.Config("bkt", prefix))
	must(t, err)
	r.c = c
	m, err := r.getManifest(context.Background(), pointerKey)
	must(t, err)
	return m
}

func touchLive(t *testing.T, home, line string) {
	t.Helper()
	p := filepath.Join(home, "raw", "acme__api", "2026-09", "live.jsonl")
	f, err := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0o600)
	must(t, err)
	_, _ = f.WriteString(line + "\n")
	must(t, f.Close())
	now := time.Now().Add(2 * time.Second)
	must(t, os.Chtimes(p, now, now))
}

func TestRunFirstThenNoChange(t *testing.T) {
	srv := s3test.New()
	defer srv.Close()
	home := setupBackup(t, srv, 5)
	sum := runOK(t, home, RunOptions{})
	if sum.Uploaded != sum.Scanned || sum.Uploaded < 5 || sum.Generation == "" || sum.NoChange {
		t.Fatalf("%+v", sum)
	}
	m := pointerOf(t, home, srv)
	if m.Generation != sum.Generation || len(m.Generations) != 1 || len(m.Files) != sum.Uploaded {
		t.Fatalf("%+v", m)
	}
	if _, ok := srv.Object("bkt", "pre/"+manifestKey(sum.Generation)); !ok {
		t.Fatal("m/<generation> missing")
	}
	puts := srv.Count("PUT")
	again := runOK(t, home, RunOptions{})
	if !again.NoChange || srv.Count("PUT") != puts {
		t.Fatalf("second run must write nothing: %+v puts=%d", again, srv.Count("PUT"))
	}
	st := LoadState(home)
	if st.LastOK == "" || st.LastError != "" || st.LastGeneration != sum.Generation {
		t.Fatalf("state %+v", st)
	}
}

func TestRunLiveGrowthMakesGenerationKeepsOld(t *testing.T) {
	srv := s3test.New()
	defer srv.Close()
	home := setupBackup(t, srv, 5)
	first := runOK(t, home, RunOptions{})
	oldLive := pointerOf(t, home, srv).Files["raw/acme__api/2026-09/live.jsonl"].Object
	touchLive(t, home, `{"role":"assistant","content":"more"}`)
	second := runOK(t, home, RunOptions{})
	if second.Uploaded != 1 || second.Deleted != 0 || second.Generation == first.Generation {
		t.Fatalf("%+v", second)
	}
	m := pointerOf(t, home, srv)
	if strings.Join(m.Generations, ",") != second.Generation+","+first.Generation {
		t.Fatal(m.Generations)
	}
	if _, ok := srv.Object("bkt", "pre/"+oldLive); !ok {
		t.Fatal("old live version must survive while its generation is kept")
	}
}

func TestRunPrunedClaimLeavesManifest(t *testing.T) {
	srv := s3test.New()
	defer srv.Close()
	home := setupBackup(t, srv, 5)
	runOK(t, home, RunOptions{})
	mds, _ := filepath.Glob(filepath.Join(home, "export", "acme__api", "*.md"))
	must(t, os.Remove(mds[0]))
	sum := runOK(t, home, RunOptions{})
	if sum.NoChange || sum.Uploaded != 0 {
		t.Fatalf("%+v", sum)
	}
	if _, ok := pointerOf(t, home, srv).Files["export/acme__api/"+filepath.Base(mds[0])]; ok {
		t.Fatal("pruned claim still in pointer")
	}
}

func TestGenerationsDropBeyondKeep(t *testing.T) {
	srv := s3test.New()
	defer srv.Close()
	home := setupBackup(t, srv, 2)
	g1 := runOK(t, home, RunOptions{}).Generation
	live1 := pointerOf(t, home, srv).Files["raw/acme__api/2026-09/live.jsonl"].Object
	sealed := pointerOf(t, home, srv).Files["raw/acme__api/2026-09/sealed.jsonl.zst"].Object
	touchLive(t, home, "2")
	g2 := runOK(t, home, RunOptions{}).Generation
	touchLive(t, home, "3")
	third := runOK(t, home, RunOptions{})
	if strings.Join(third.Dropped, ",") != g1 || third.Deleted != 1 {
		t.Fatalf("%+v", third)
	}
	m := pointerOf(t, home, srv)
	if strings.Join(m.Generations, ",") != third.Generation+","+g2 || slices.Contains(m.Generations, g1) {
		t.Fatalf("%+v", m)
	}
	if _, ok := srv.Object("bkt", "pre/"+live1); ok {
		t.Fatal("generation 1's unique live part must be deleted")
	}
	if _, ok := srv.Object("bkt", "pre/"+sealed); !ok {
		t.Fatal("shared sealed part must stay")
	}
	if _, ok := srv.Object("bkt", "pre/"+manifestKey(g1)); ok {
		t.Fatal("m/g1 must be deleted")
	}
}

func TestDroppingFinishesOnNextRun(t *testing.T) {
	srv := s3test.New()
	defer srv.Close()
	home := setupBackup(t, srv, 1)
	g1 := runOK(t, home, RunOptions{}).Generation
	live1 := pointerOf(t, home, srv).Files["raw/acme__api/2026-09/live.jsonl"].Object
	srv.FailNext("bkt", "pre/"+live1, "500", 4) // one full retry cycle
	touchLive(t, home, "2")
	second := runOK(t, home, RunOptions{})
	m := pointerOf(t, home, srv)
	if strings.Join(m.Dropping, ",") != g1 || len(second.Dropped) != 0 {
		t.Fatalf("pointer must record the pending drop: %+v %+v", m, second)
	}
	if _, ok := srv.Object("bkt", "pre/"+manifestKey(g1)); !ok {
		t.Fatal("m/g1 must survive a failed drop")
	}
	third := runOK(t, home, RunOptions{}) // no change, but finishes the drop
	if !third.NoChange || strings.Join(third.Dropped, ",") != g1 {
		t.Fatalf("%+v", third)
	}
	if _, ok := srv.Object("bkt", "pre/"+manifestKey(g1)); ok {
		t.Fatal("m/g1 must be gone")
	}
	touchLive(t, home, "3")
	runOK(t, home, RunOptions{})
	if m := pointerOf(t, home, srv); slices.Contains(m.Dropping, g1) {
		t.Fatalf("a finished drop must not be re-listed: %v", m.Dropping)
	}
}

func TestCommitPointFailedPointerKeepsPrevious(t *testing.T) {
	srv := s3test.New()
	defer srv.Close()
	home := setupBackup(t, srv, 5)
	g1 := runOK(t, home, RunOptions{}).Generation
	touchLive(t, home, "2")
	srv.FailNext("bkt", "pre/manifest", "503", 4) // one full retry cycle
	if _, err := Run(context.Background(), home, RunOptions{}); err == nil {
		t.Fatal("pointer PUT failure must fail the run")
	}
	if m := pointerOf(t, home, srv); m.Generation != g1 {
		t.Fatal("previous pointer must be intact")
	}
	if st := LoadState(home); st.LastError == "" {
		t.Fatal("last_error must be stamped")
	}
	sum := runOK(t, home, RunOptions{})
	if sum.Uploaded != 1 {
		t.Fatalf("orphan object must be re-PUT so it becomes referenced: %+v", sum)
	}
}

func TestWriterGuardAndTakeOver(t *testing.T) {
	srv := s3test.New()
	defer srv.Close()
	homeA := setupBackup(t, srv, 5)
	runOK(t, homeA, RunOptions{})
	homeB, _ := seedHome(t)
	must(t, copyFile(filepath.Join(homeA, "backup.env"), filepath.Join(homeB, "backup.env")))
	must(t, copyFile(filepath.Join(homeA, "backup.key"), filepath.Join(homeB, "backup.key")))
	puts := srv.Count("PUT")
	var ow *OtherWriterError
	if _, err := Run(context.Background(), homeB, RunOptions{}); !errors.As(err, &ow) {
		t.Fatalf("want OtherWriterError, got %v", err)
	}
	if srv.Count("PUT") != puts {
		t.Fatal("guard must refuse before uploading")
	}
	runOK(t, homeB, RunOptions{TakeOver: true})
	if _, err := Run(context.Background(), homeA, RunOptions{}); !errors.As(err, &ow) {
		t.Fatalf("A must now refuse, got %v", err)
	}

	// The adoption B used above is consumed by that successful write: A can
	// take back over, but once A has written, B is just another install and
	// must take over explicitly again rather than riding A's old adoption.
	runOK(t, homeA, RunOptions{TakeOver: true})
	if _, err := Run(context.Background(), homeB, RunOptions{}); !errors.As(err, &ow) {
		t.Fatalf("B must refuse once A has taken back over, got %v", err)
	}
	runOK(t, homeB, RunOptions{TakeOver: true})
	if _, err := Run(context.Background(), homeA, RunOptions{}); !errors.As(err, &ow) {
		t.Fatalf("A must refuse again, got %v", err)
	}
}

// TestRestoreAdoptionIsConsumedByFirstWrite: a restore adopts the writer it
// restored from, so the newly restored machine can back up right away. But
// that adoption must not linger forever: a no-change run keeps it (the
// restored machine may still need it for its first real write), while the
// first run that actually writes the pointer consumes it. After that, the
// old writer must --take-over explicitly, like any other install.
func TestRestoreAdoptionIsConsumedByFirstWrite(t *testing.T) {
	srv := s3test.New()
	defer srv.Close()
	homeA := setupBackup(t, srv, 5)
	runOK(t, homeA, RunOptions{})

	homeB := freshTarget(t, homeA)
	if _, err := Restore(context.Background(), homeB, RestoreOptions{Health: noDaemon}); err != nil {
		t.Fatal(err)
	}

	noChange := runOK(t, homeB, RunOptions{})
	if !noChange.NoChange {
		t.Fatalf("restored store must be in sync: %+v", noChange)
	}
	if adopted := LoadState(homeB).Adopted; len(adopted) != 1 {
		t.Fatalf("a no-change run must not consume the adoption: %v", adopted)
	}

	touchLive(t, homeB, `{"role":"assistant","content":"more"}`)
	runOK(t, homeB, RunOptions{})
	if adopted := LoadState(homeB).Adopted; len(adopted) != 0 {
		t.Fatalf("the first real write must consume the adoption: %v", adopted)
	}

	runOK(t, homeA, RunOptions{TakeOver: true})
	var ow *OtherWriterError
	if _, err := Run(context.Background(), homeB, RunOptions{}); !errors.As(err, &ow) {
		t.Fatalf("B must refuse once A has taken back over, got %v", err)
	}
}

func TestDryRunWritesNothing(t *testing.T) {
	srv := s3test.New()
	defer srv.Close()
	home := setupBackup(t, srv, 5)
	sum := runOK(t, home, RunOptions{DryRun: true})
	if !sum.DryRun || srv.Count("PUT") != 0 {
		t.Fatalf("%+v puts=%d", sum, srv.Count("PUT"))
	}
	if _, err := os.Stat(filepath.Join(home, "backup-state.json")); !os.IsNotExist(err) {
		t.Fatal("dry run must not write state")
	}
}

func TestRunRefusesWhenLocked(t *testing.T) {
	srv := s3test.New()
	defer srv.Close()
	home := setupBackup(t, srv, 5)
	release, err := Lock(home)
	must(t, err)
	defer release()
	if _, err := Run(context.Background(), home, RunOptions{}); !errors.Is(err, ErrLocked) {
		t.Fatalf("want ErrLocked, got %v", err)
	}
}

// TestPointerForbiddenGetsListBucketHint covers finding 4: a 403 on the
// first pointer GET (AWS without s3:ListBucket) must not be treated as
// not-found, and the error must hint at the missing permission. Restore and
// List share the same hint, since they hit the same pointer GET.
func TestPointerForbiddenGetsListBucketHint(t *testing.T) {
	srv := s3test.New()
	defer srv.Close()
	home := setupBackup(t, srv, 5)

	srv.FailNext("bkt", "pre/manifest", "403", 1)
	_, err := Run(context.Background(), home, RunOptions{})
	if err == nil || !strings.Contains(err.Error(), "s3:ListBucket") {
		t.Fatalf("Run: want a 403 hint mentioning s3:ListBucket, got %v", err)
	}

	srv.FailNext("bkt", "pre/manifest", "403", 1)
	if _, err := List(context.Background(), home); err == nil || !strings.Contains(err.Error(), "s3:ListBucket") {
		t.Fatalf("List: want a 403 hint mentioning s3:ListBucket, got %v", err)
	}

	dst := freshTarget(t, home)
	srv.FailNext("bkt", "pre/manifest", "403", 1)
	if _, err := Restore(context.Background(), dst, RestoreOptions{Health: noDaemon}); err == nil || !strings.Contains(err.Error(), "s3:ListBucket") {
		t.Fatalf("Restore: want a 403 hint mentioning s3:ListBucket, got %v", err)
	}
}

// TestCacheHitRequiresPointerMatchForLiveAndSqlite is the Critical
// regression test (finding 1): a stat-cache hit for a live part or sqlite
// file must not skip the copy/snapshot step unless the bucket pointer
// already holds that hash. Run 2 targets a fresh prefix in the same bucket
// (pointer empty) while the on-disk cache from run 1 is untouched, so a
// cache hit is guaranteed but nothing has actually been uploaded there yet.
// Without the fix, the raw on-disk claims.sqlite main file is uploaded
// (its rows are still only in the WAL), and restoring it yields zero
// claims.
func TestCacheHitRequiresPointerMatchForLiveAndSqlite(t *testing.T) {
	srv := s3test.New()
	defer srv.Close()
	home := setupBackup(t, srv, 5)
	runOK(t, home, RunOptions{}) // run 1: prefix "pre"; local cache now holds every file's hash

	// Same bucket, same backup.key, a new prefix the pointer has never seen.
	env := fmt.Sprintf("LOSSLESS_BACKUP_URL=s3://bkt/pre2\nLOSSLESS_BACKUP_ENDPOINT=%s\n", srv.URL())
	must(t, os.WriteFile(filepath.Join(home, "backup.env"), []byte(env), 0o600))

	runOK(t, home, RunOptions{}) // run 2: cache hits everywhere, but the pointer for pre2 is empty

	pointer := pointerAtPrefix(t, home, "pre2", srv)
	entry, ok := pointer.Files["index/claims.sqlite"]
	if !ok {
		t.Fatal("index/claims.sqlite missing from the new prefix's pointer")
	}

	snap := filepath.Join(t.TempDir(), "claims.sqlite")
	must(t, store.Snapshot(filepath.Join(home, "index", "claims.sqlite"), snap))
	wantSHA, _, err := hashFile(snap)
	must(t, err)
	if entry.SHA256 != wantSHA {
		t.Fatalf("uploaded index/claims.sqlite sha = %s, want a WAL-consistent snapshot hash %s", entry.SHA256, wantSHA)
	}
	rawSHA, _, err := hashFile(filepath.Join(home, "index", "claims.sqlite"))
	must(t, err)
	if entry.SHA256 == rawSHA {
		t.Fatal("uploaded index/claims.sqlite sha matches the on-disk main file, not a snapshot: WAL rows would be lost")
	}

	dst := freshTarget(t, home)
	sum, err := Restore(context.Background(), dst, RestoreOptions{Health: noDaemon})
	must(t, err)
	if sum.Generation != pointer.Generation {
		t.Fatalf("restore generation = %s, want %s", sum.Generation, pointer.Generation)
	}
	if strings.Join(claimTexts(t, dst), "|") != strings.Join(claimTexts(t, home), "|") {
		t.Fatal("claims differ after restore: WAL rows were not in the uploaded snapshot")
	}
}

func TestPointerRateLimitIsRetried(t *testing.T) {
	srv := s3test.New()
	defer srv.Close()
	srv.RateLimit = true
	home := setupBackup(t, srv, 5)
	sleepHook = time.Sleep // the 1s backoff is what clears R2's per-key limit
	runOK(t, home, RunOptions{})
	touchLive(t, home, "2")
	runOK(t, home, RunOptions{})
}

// A drop left pending by an earlier run must not take objects the pointer
// still references. Before the fix the pending phase protected only the
// generations kept after this run, so with keep=1 every object shared by
// the pending generation and the current one was deleted before the new
// pointer was written, and nothing re-uploaded them.
func TestPendingDropKeepsObjectsThePointerStillReferences(t *testing.T) {
	srv := s3test.New()
	defer srv.Close()
	home := setupBackup(t, srv, 1)
	g1 := runOK(t, home, RunOptions{}).Generation
	live1 := pointerOf(t, home, srv).Files["raw/acme__api/2026-09/live.jsonl"].Object
	srv.FailNext("bkt", "pre/"+live1, "500", 4) // g1's drop is left pending
	touchLive(t, home, "2")
	runOK(t, home, RunOptions{})
	if m := pointerOf(t, home, srv); strings.Join(m.Dropping, ",") != g1 {
		t.Fatalf("precondition: g1 pending, got %v", m.Dropping)
	}
	touchLive(t, home, "3")
	runOK(t, home, RunOptions{}) // finishes g1's drop first, then pushes g2 out
	m := pointerOf(t, home, srv)
	for rel, e := range m.Files {
		if _, ok := srv.Object("bkt", "pre/"+e.Object); !ok {
			t.Fatalf("%s: the pointer references %s but the bucket no longer has it", rel, e.Object)
		}
	}
	if _, ok := srv.Object("bkt", "pre/"+live1); ok {
		t.Fatal("g1's own live object must go once its drop finishes")
	}
	if slices.Contains(m.Dropping, g1) {
		t.Fatalf("finished drop re-listed: %v", m.Dropping)
	}
}

func TestConfigErrorStampsLastError(t *testing.T) {
	srv := s3test.New()
	defer srv.Close()
	home := setupBackup(t, srv, 5)
	must(t, os.Remove(filepath.Join(home, "backup.key")))
	_, err := Run(context.Background(), home, RunOptions{})
	var ce *ConfigError
	if !errors.As(err, &ce) {
		t.Fatalf("want ConfigError, got %v", err)
	}
	st := LoadState(home)
	if !strings.Contains(st.LastError, "backup.key") || st.LastAttempt == "" {
		t.Fatalf("state %+v", st)
	}
	if ok, detail := DoctorCheck(home, time.Now()); ok || !strings.Contains(detail, "last attempt failed") || !strings.Contains(detail, "backup.key") {
		t.Fatalf("doctor %v %q", ok, detail)
	}
}

func TestEmptyStoreIsNotABackup(t *testing.T) {
	srv := s3test.New()
	defer srv.Close()
	clearBackupEnv(t)
	t.Setenv("LOSSLESS_BACKUP_ACCESS_KEY", "test")
	t.Setenv("LOSSLESS_BACKUP_SECRET_KEY", "secret")
	home := t.TempDir()
	must(t, Init(home, InitOptions{URL: "s3://bkt/pre", Endpoint: srv.URL(), Every: time.Hour}))
	_, err := Run(context.Background(), home, RunOptions{})
	if !errors.Is(err, ErrEmptyStore) {
		t.Fatalf("want ErrEmptyStore, got %v", err)
	}
	if st := LoadState(home); st.LastOK != "" {
		t.Fatalf("an empty store must not count as a successful backup: %+v", st)
	}
	if ok, _ := DoctorCheck(home, time.Now()); ok {
		t.Fatal("doctor must not report ok")
	}
}

// export/*.md and sealed parts are read in place; a supersede or a project
// removal can delete one between the walk and the upload.
func TestUploadOneSkipsVanishedDirectItem(t *testing.T) {
	it := Item{Rel: "export/acme__api/gone.md", Src: filepath.Join(t.TempDir(), "gone.md"), SHA256: strings.Repeat("0", 64)}
	got, _, err := uploadOne(context.Background(), nil, t.TempDir(), it)
	if err != nil || !got.Gone {
		t.Fatalf("vanished export file must be skipped: gone=%v err=%v", got.Gone, err)
	}
	it.Temp = true
	if _, _, err := uploadOne(context.Background(), nil, t.TempDir(), it); err == nil {
		t.Fatal("a vanished temp copy is a bug, not a skip")
	}
}
