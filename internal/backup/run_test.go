// internal/backup/run_test.go
package backup

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"lossless/internal/backup/s3"
	"lossless/internal/backup/s3/s3test"
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
	t.Helper()
	keys, err := LoadKey(home)
	must(t, err)
	r := &remote{keys: keys}
	c, err := s3.New(srv.Config("bkt", "pre"))
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
	if strings.Join(m.Generations, ",") != third.Generation+","+g2 || containsString(m.Generations, g1) {
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
	if m := pointerOf(t, home, srv); containsString(m.Dropping, g1) {
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
