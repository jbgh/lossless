// internal/backup/live_test.go
package backup

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"lossless/internal/backup/s3"
)

// TestLiveR2 runs a real backup and restore against a bucket you own.
// Skipped unless LOSSLESS_BACKUP_LIVE_TEST=1 and the usual LOSSLESS_BACKUP_*
// variables point at the bucket. Everything it writes goes under a random
// prefix and is deleted at the end.
//
//	LOSSLESS_BACKUP_LIVE_TEST=1 LOSSLESS_BACKUP_URL=s3://bkt/lossless-test \
//	LOSSLESS_BACKUP_ENDPOINT=https://<acct>.r2.cloudflarestorage.com \
//	LOSSLESS_BACKUP_ACCESS_KEY=… LOSSLESS_BACKUP_SECRET_KEY=… \
//	go test ./internal/backup/ -run TestLiveR2 -v
func TestLiveR2(t *testing.T) {
	if os.Getenv("LOSSLESS_BACKUP_LIVE_TEST") != "1" {
		t.Skip("set LOSSLESS_BACKUP_LIVE_TEST=1 and LOSSLESS_BACKUP_* to run")
	}
	base := os.Getenv("LOSSLESS_BACKUP_URL")
	if base == "" {
		t.Fatal("LOSSLESS_BACKUP_URL is required")
	}
	url := strings.TrimRight(base, "/") + "/" + NewGeneration(time.Now())
	t.Setenv("LOSSLESS_BACKUP_URL", url)
	home, _ := seedHome(t)
	must(t, Init(home, InitOptions{URL: url, Endpoint: os.Getenv("LOSSLESS_BACKUP_ENDPOINT"), Region: os.Getenv("LOSSLESS_BACKUP_REGION"), Every: 0, Keep: 2}))
	ctx := context.Background()

	cleanup := func() {
		cfg, err := LoadConfig(home)
		if err != nil {
			return
		}
		rm, err := newRemote(cfg, home)
		if err != nil {
			return
		}
		pointer, err := rm.getManifest(ctx, pointerKey)
		if err != nil {
			return
		}
		for _, g := range append(append([]string{}, pointer.Generations...), pointer.Dropping...) {
			m, err := rm.getManifest(ctx, manifestKey(g))
			if err == nil {
				for _, e := range m.Files {
					_ = rm.c.Delete(ctx, e.Object)
				}
			}
			_ = rm.c.Delete(ctx, manifestKey(g))
		}
		for _, e := range pointer.Files {
			_ = rm.c.Delete(ctx, e.Object)
		}
		_ = rm.c.Delete(ctx, pointerKey)
	}
	t.Cleanup(cleanup)

	first := runOK(t, home, RunOptions{Verbose: true, Out: os.Stderr})
	if first.Uploaded == 0 {
		t.Fatal("nothing uploaded")
	}
	if again := runOK(t, home, RunOptions{}); !again.NoChange {
		t.Fatalf("second run must be no change: %+v", again)
	}
	touchLive(t, home, `{"role":"assistant","content":"live grew"}`)
	second := runOK(t, home, RunOptions{})
	touchLive(t, home, `{"role":"assistant","content":"live grew again"}`)
	third := runOK(t, home, RunOptions{})
	if strings.Join(third.Dropped, ",") != first.Generation {
		t.Fatalf("keep=2 must drop the first generation: %+v", third)
	}
	gens, err := List(ctx, home)
	if err != nil || len(gens) != 2 || gens[0].Generation != third.Generation || gens[1].Generation != second.Generation {
		t.Fatalf("%+v %v", gens, err)
	}

	dst := freshTarget(t, home)
	sum, err := Restore(ctx, dst, RestoreOptions{Health: noDaemon})
	if err != nil || sum.Restored != third.Scanned {
		t.Fatalf("%+v %v", sum, err)
	}
	if strings.Join(claimTexts(t, dst), "|") != strings.Join(claimTexts(t, home), "|") {
		t.Fatal("claims differ after live restore")
	}
	if _, err := s3.New(s3.Config{Bucket: "x", Endpoint: os.Getenv("LOSSLESS_BACKUP_ENDPOINT")}); err != nil {
		t.Fatal(err)
	}
}
