// internal/backup/restore.go
package backup

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"lossless/internal/backup/s3"
	"lossless/internal/env"
	"lossless/internal/store"
)

var (
	ErrDaemonRunning    = errors.New("lossless serve is running; stop it before restore")
	ErrStoreNotEmpty    = errors.New("store already has raw or export files; use --force to union")
	ErrNothingToRestore = errors.New("nothing to restore: the bucket has no manifest")
)

// claimsRel is the one restored file lossless ever opens as a live
// database (internal/store.Open, in WAL mode).
const claimsRel = "index/claims.sqlite"

type RestoreOptions struct {
	Force  bool
	At     string
	Out    io.Writer
	Health func(url string) bool // nil = real probe
}

type RestoreSummary struct {
	Generation string
	Restored   int
	Skipped    int
	LeftAlone  int
	Bytes      int64
	Left       []string
}

func probeHealth(url string) bool {
	c := &http.Client{Timeout: 2 * time.Second}
	res, err := c.Get(strings.TrimRight(url, "/") + "/health")
	if err != nil {
		return false
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 4096))
	_ = res.Body.Close()
	return res.StatusCode == 200
}

func hasRegularFile(dir string) bool {
	found := false
	_ = filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.Type().IsRegular() {
			found = true
			return fs.SkipAll
		}
		return nil
	})
	return found
}

// Restore pulls one generation (latest, or o.At) into home.
func Restore(ctx context.Context, home string, o RestoreOptions) (RestoreSummary, error) {
	var sum RestoreSummary
	if o.Out == nil {
		o.Out = io.Discard
	}
	if o.Health == nil {
		o.Health = probeHealth
	}
	if url := env.Sidecar(); url == "" {
		fmt.Fprintln(o.Out, "warning: LOSSLESS_SIDECAR is off, cannot check for a running daemon; stop lossless serve before restoring")
	} else if o.Health(url) {
		return sum, ErrDaemonRunning
	}
	if !o.Force && (hasRegularFile(filepath.Join(home, "raw")) || hasRegularFile(filepath.Join(home, "export"))) {
		return sum, ErrStoreNotEmpty
	}
	cfg, err := LoadConfig(home)
	if err != nil {
		return sum, &ConfigError{Err: err}
	}
	rm, err := newRemote(cfg, home)
	if err != nil {
		return sum, &ConfigError{Err: err}
	}
	release, err := Lock(home)
	if err != nil {
		return sum, err
	}
	defer release()

	pointer, err := rm.getManifest(ctx, pointerKey)
	if errors.Is(err, s3.ErrNotFound) {
		return sum, ErrNothingToRestore
	}
	if err != nil {
		return sum, err
	}
	m := pointer
	if o.At != "" && o.At != pointer.Generation {
		if containsString(pointer.Dropping, o.At) || !containsString(pointer.Generations, o.At) {
			return sum, fmt.Errorf("generation %s is not kept; kept: %s", o.At, strings.Join(pointer.Generations, ", "))
		}
		if m, err = rm.getManifest(ctx, manifestKey(o.At)); err != nil {
			return sum, err
		}
	}
	sum.Generation = m.Generation

	rels := make([]string, 0, len(m.Files))
	for rel := range m.Files {
		rels = append(rels, rel)
	}
	sort.Strings(rels)

	// cache is the local backup-state as it stood before this restore. It
	// lets restoreOne skip a byte-identical re-download on a repeat restore
	// even for index/claims.sqlite, whose on-disk bytes legitimately move
	// once lossless opens it live (see the settle step below) even though
	// nothing in it actually changed.
	cache := LoadState(home)
	state := &State{Files: map[string]FileState{}, Manifests: map[string]*Manifest{}}
	var (
		mu       sync.Mutex
		wg       sync.WaitGroup
		firstErr error
	)
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	sem := make(chan struct{}, parallel)
	for _, rel := range rels {
		if ctx.Err() != nil {
			break
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(rel string, e FileEntry) {
			defer wg.Done()
			defer func() { <-sem }()
			outcome, n, err := restoreOne(ctx, rm, home, rel, e, cache)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				if firstErr == nil {
					firstErr = fmt.Errorf("%s: %w", rel, err)
					cancel()
				}
				return
			}
			switch outcome {
			case "restored":
				sum.Restored++
				sum.Bytes += n
			case "skipped":
				sum.Skipped++
			case "left":
				sum.LeftAlone++
				sum.Left = append(sum.Left, rel)
				return
			}
			if st, err := os.Stat(filepath.Join(home, filepath.FromSlash(rel))); err == nil {
				state.Files[rel] = FileState{Size: e.Size, StatSize: st.Size(), Mtime: st.ModTime().UnixNano(), SHA256: e.SHA256, Object: e.Object}
			}
		}(rel, m.Files[rel])
	}
	wg.Wait()
	if firstErr != nil {
		return sum, firstErr
	}
	if _, ok := m.Files[claimsRel]; ok {
		if err := settleClaims(home, state); err != nil {
			return sum, err
		}
	}
	sort.Strings(sum.Left)
	state.Adopt(pointer.Client)
	state.Manifests[m.Generation] = m
	if err := state.Save(home); err != nil {
		return sum, err
	}
	return sum, nil
}

// restoreOne returns "restored", "skipped" (already at this hash), or
// "left" (a differing raw/export file that is never overwritten).
func restoreOne(ctx context.Context, rm *remote, home, rel string, e FileEntry, cache *State) (string, int64, error) {
	target := filepath.Join(home, filepath.FromSlash(rel))
	if st, err := os.Stat(target); err == nil && st.Mode().IsRegular() {
		// Trust a cache entry that already matches this file's hash and
		// stat: a repeat restore of index/claims.sqlite would otherwise
		// always re-download it, since settleClaims (below) leaves its
		// on-disk bytes past the object's own hash the moment lossless
		// opens it live.
		if c, ok := cache.Files[rel]; ok && c.SHA256 == e.SHA256 && c.StatSize == st.Size() && c.Mtime == st.ModTime().UnixNano() {
			return "skipped", 0, nil
		}
		if sha, _, err := hashFile(target); err == nil && sha == e.SHA256 {
			return "skipped", 0, nil
		}
		if !strings.HasPrefix(rel, "index/") {
			return "left", 0, nil
		}
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		return "", 0, err
	}
	body, _, err := rm.c.Get(ctx, e.Object)
	if err != nil {
		return "", 0, err
	}
	defer body.Close()
	tmp := target + ".restore-tmp"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return "", 0, err
	}
	sha, derr := rm.keys.Decrypt(out, body, rel)
	if cerr := out.Close(); derr == nil {
		derr = cerr
	}
	if derr != nil {
		_ = os.Remove(tmp)
		return "", 0, derr
	}
	if sha != e.SHA256 {
		_ = os.Remove(tmp)
		return "", 0, fmt.Errorf("hash mismatch after decrypt")
	}
	if strings.HasSuffix(rel, ".sqlite") {
		// A leftover WAL from the empty store would corrupt the snapshot.
		_ = os.Remove(target + "-wal")
		_ = os.Remove(target + "-shm")
	}
	if err := os.Rename(tmp, target); err != nil {
		_ = os.Remove(tmp)
		return "", 0, err
	}
	return "restored", e.Size, nil
}

// settleClaims opens and closes the just-restored claims.sqlite once via
// the real store package. Every backed-up generation of it is a VACUUM
// INTO snapshot, always written in legacy (rollback) journal format;
// internal/store.Open always requests WAL. The first time anything opens
// a restored snapshot live, SQLite converts it to WAL in place, changing
// a few header bytes with no row-level change. Doing that conversion here
// means the daemon's own first open finds nothing left to do, and the
// state entry recorded right after (trusted, not re-hashed, by both
// restoreOne above and walk's cache on the next backup run) stays valid.
func settleClaims(home string, state *State) error {
	st, err := store.Open(home)
	if err != nil {
		return err
	}
	if err := st.Close(); err != nil {
		return err
	}
	fi, err := os.Stat(filepath.Join(home, filepath.FromSlash(claimsRel)))
	if err != nil {
		return err
	}
	fe, ok := state.Files[claimsRel]
	if !ok {
		return nil
	}
	fe.StatSize = fi.Size()
	fe.Mtime = fi.ModTime().UnixNano()
	state.Files[claimsRel] = fe
	return nil
}

type GenerationInfo struct {
	Generation string
	CreatedAt  string
	Lossless   string
	Client     string
	Files      int
	Bytes      int64
	Latest     bool
}

// List describes the kept generations, newest first.
func List(ctx context.Context, home string) ([]GenerationInfo, error) {
	cfg, err := LoadConfig(home)
	if err != nil {
		return nil, &ConfigError{Err: err}
	}
	rm, err := newRemote(cfg, home)
	if err != nil {
		return nil, &ConfigError{Err: err}
	}
	pointer, err := rm.getManifest(ctx, pointerKey)
	if errors.Is(err, s3.ErrNotFound) {
		return nil, ErrNothingToRestore
	}
	if err != nil {
		return nil, err
	}
	var out []GenerationInfo
	for _, g := range pointer.Generations {
		if containsString(pointer.Dropping, g) {
			continue
		}
		m := pointer
		if g != pointer.Generation {
			m, err = rm.getManifest(ctx, manifestKey(g))
			if errors.Is(err, s3.ErrNotFound) {
				continue
			}
			if err != nil {
				return nil, err
			}
		}
		info := GenerationInfo{Generation: m.Generation, CreatedAt: m.CreatedAt, Lossless: m.Lossless, Client: m.Client, Files: len(m.Files), Latest: g == pointer.Generation}
		for _, e := range m.Files {
			info.Bytes += e.Size
		}
		out = append(out, info)
	}
	return out, nil
}
