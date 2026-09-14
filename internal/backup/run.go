// internal/backup/run.go
package backup

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"lossless/internal/backup/s3"
	"lossless/internal/version"
	"lossless/internal/write"
)

const parallel = 4

// sleepHook replaces the retry sleeper when set. Tests use it.
var sleepHook func(time.Duration)

type RunOptions struct {
	DryRun   bool
	Verbose  bool
	TakeOver bool
	Out      io.Writer
	Now      func() time.Time
}

type Summary struct {
	Scanned    int
	Uploaded   int
	Deleted    int
	Bytes      int64
	Generation string
	NoChange   bool
	DryRun     bool
	Dropped    []string
	Elapsed    time.Duration
}

// OtherWriterError: the pointer was last written by an install this one
// has not adopted.
type OtherWriterError struct {
	Client    string
	CreatedAt string
}

func (e *OtherWriterError) Error() string {
	return fmt.Sprintf("bucket last written by %s at %s; run lossless backup --take-over if that machine is retired", e.Client, e.CreatedAt)
}

func newRemote(cfg *Config, home string) (*remote, error) {
	keys, err := LoadKey(home)
	if err != nil {
		return nil, err
	}
	c, err := s3.New(cfg.S3)
	if err != nil {
		return nil, err
	}
	if sleepHook != nil {
		c.SetSleep(sleepHook)
	}
	return &remote{c: c, keys: keys}, nil
}

// Run performs one backup. It is single-flight per home. State is saved
// on success and on failure (to stamp last_error), never on a dry run.
func Run(ctx context.Context, home string, o RunOptions) (Summary, error) {
	if o.Out == nil {
		o.Out = io.Discard
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	start := o.Now().UTC()
	var sum Summary
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
	state := LoadState(home)
	state.LastAttempt = start.Format(time.RFC3339)
	err = run(ctx, home, cfg, rm, state, o, start, &sum)
	if !o.DryRun {
		if err != nil {
			state.LastError = err.Error()
		} else {
			state.LastOK = o.Now().UTC().Format(time.RFC3339)
			state.LastError = ""
		}
		if serr := state.Save(home); serr != nil && err == nil {
			err = serr
		}
	}
	sum.Elapsed = o.Now().Sub(start)
	return sum, err
}

func run(ctx context.Context, home string, cfg *Config, rm *remote, state *State, o RunOptions, start time.Time, sum *Summary) error {
	me := write.ClientID(home)
	pointer, err := rm.getManifest(ctx, pointerKey)
	if errors.Is(err, s3.ErrNotFound) {
		pointer = emptyManifest()
	} else if err != nil {
		var se *s3.StatusError
		if errors.As(err, &se) && se.Status == 403 {
			return fmt.Errorf("%w (a 403 on the first GET usually means the credentials lack s3:ListBucket on the bucket; see docs/deploy.md)", err)
		}
		return err
	}
	if o.TakeOver {
		state.Adopt(pointer.Client)
	}
	if !state.AllowsWriter(me, pointer.Client) {
		return &OtherWriterError{Client: pointer.Client, CreatedAt: pointer.CreatedAt}
	}

	tmp := tmpDir(home)
	_ = os.RemoveAll(tmp)
	if err := os.MkdirAll(tmp, 0o700); err != nil {
		return err
	}
	defer os.RemoveAll(tmp)

	items, err := walk(home, tmp, rm.keys, state, pointer)
	if err != nil {
		return err
	}
	sum.Scanned = len(items)
	gen := NewGeneration(start)
	p := makePlan(items, pointer, cfg.Keep, gen)

	if o.DryRun {
		sum.DryRun = true
		printPlan(o.Out, p, gen, pointer.Dropping)
		return nil
	}

	// Finish drops a previous run left pending before anything else.
	kept := p.Kept
	if !p.NoChange {
		kept = p.Kept[1:] // existing generations; the new one has no manifest yet
	}
	pending := dropGenerations(ctx, rm, state, kept, nil, pointer.Dropping, o.Out, sum)

	current := pointer
	if p.NoChange {
		sum.NoChange = true
		fmt.Fprintln(o.Out, "no change")
	} else {
		if err := uploadAll(ctx, rm, tmp, items, p.Upload, o, sum); err != nil {
			return err
		}
		current = &Manifest{
			Version:     manifestVersion,
			Generation:  gen,
			CreatedAt:   start.Format(time.RFC3339),
			Lossless:    version.Version,
			Client:      me,
			Generations: p.Kept,
			Dropping:    append(append([]string{}, pending...), p.Drop...),
			Files:       filesFrom(items),
		}
		if err := rm.putManifest(ctx, manifestKey(gen), current); err != nil {
			return err
		}
		if err := rm.putManifest(ctx, pointerKey, current); err != nil {
			return err
		}
		state.Manifests[gen] = current
		state.LastGeneration = gen
		sum.Generation = gen
		dropGenerations(ctx, rm, state, p.Kept, current, p.Drop, o.Out, sum)
	}

	state.Files = make(map[string]FileState, len(items))
	for _, it := range items {
		state.Files[it.Rel] = FileState{Size: it.Size, StatSize: it.StatSize, Mtime: it.Mtime, SHA256: it.SHA256, Object: it.Object}
	}
	for g := range state.Manifests {
		if !containsString(current.Generations, g) {
			delete(state.Manifests, g)
		}
	}
	return nil
}

func containsString(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func printPlan(out io.Writer, p plan, gen string, pending []string) {
	fmt.Fprintf(out, "dry run: generation %s\n", gen)
	if p.NoChange {
		fmt.Fprintln(out, "no change")
	}
	for _, it := range p.Upload {
		fmt.Fprintf(out, "upload  %s (%d bytes)\n", it.Rel, it.Size)
	}
	for _, rel := range p.Removed {
		fmt.Fprintf(out, "remove  %s\n", rel)
	}
	for _, g := range append(append([]string{}, pending...), p.Drop...) {
		fmt.Fprintf(out, "drop    %s\n", g)
	}
}

// uploadAll encrypts and uploads up to four items at a time. See uploadOne
// for what happens when the bytes read do not match it.SHA256.
func uploadAll(ctx context.Context, rm *remote, tmp string, all []Item, todo []Item, o RunOptions, sum *Summary) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	index := map[string]int{}
	for i, it := range all {
		index[it.Rel] = i
	}
	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		firstErr error
	)
	sem := make(chan struct{}, parallel)
	for _, it := range todo {
		if ctx.Err() != nil {
			break
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(it Item) {
			defer wg.Done()
			defer func() { <-sem }()
			fixed, n, err := uploadOne(ctx, rm, tmp, it)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				if firstErr == nil {
					firstErr = err
					cancel()
				}
				return
			}
			all[index[it.Rel]] = fixed
			sum.Uploaded++
			sum.Bytes += n
			if o.Verbose {
				fmt.Fprintf(o.Out, "upload  %s (%d bytes)\n", it.Rel, fixed.Size)
			}
		}(it)
	}
	wg.Wait()
	return firstErr
}

type countReader struct {
	r io.Reader
	n int64
}

func (c *countReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	return n, err
}

func uploadOne(ctx context.Context, rm *remote, tmp string, it Item) (Item, int64, error) {
	src, err := os.Open(it.Src)
	if err != nil {
		return it, 0, err
	}
	defer src.Close()
	enc := filepath.Join(tmp, rm.keys.Name(it.Rel)+"."+it.SHA256[:16]+".enc")
	out, err := os.OpenFile(enc, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return it, 0, err
	}
	defer os.Remove(enc)
	cr := &countReader{r: src}
	plainSHA, cipherSHA, n, err := rm.keys.Encrypt(out, cr, it.Rel)
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return it, 0, err
	}
	if plainSHA != it.SHA256 {
		// A Temp item is a copy or snapshot made for this run alone; its
		// bytes cannot legitimately change between the hash and the
		// upload, so a mismatch means the walk's cache guard was bypassed
		// (or the temp file was tampered with) and must not be uploaded
		// under a re-keyed name. export/*.md is written tmp-and-rename, so
		// an in-place read can legitimately race a supersede between hash
		// and upload; re-key it so the manifest and object name agree.
		if it.Temp || !strings.HasSuffix(it.Rel, ".md") {
			return it, 0, fmt.Errorf("%s: read as sha256 %s but hashed %s at upload time; refusing to upload", it.Rel, it.SHA256, plainSHA)
		}
		it.SHA256 = plainSHA
		it.Size = cr.n
		it.Object = objectKey(rm.keys.Name(it.Rel), plainSHA)
	}
	f, err := os.Open(enc)
	if err != nil {
		return it, 0, err
	}
	defer f.Close()
	if err := rm.c.Put(ctx, it.Object, f, n, cipherSHA); err != nil {
		return it, 0, err
	}
	return it, n, nil
}

// dropGenerations deletes the objects that only the dropped generations
// reference, then their manifests. kept lists the generations whose
// objects must survive; current (may be nil) is the just-written manifest
// that is not yet in the state cache. It returns the generations whose
// removal did not finish; the pointer keeps listing those under Dropping.
func dropGenerations(ctx context.Context, rm *remote, state *State, kept []string, current *Manifest, drop []string, out io.Writer, sum *Summary) (remaining []string) {
	if len(drop) == 0 {
		return nil
	}
	refs := map[string]bool{}
	for _, g := range kept {
		m := state.Manifests[g]
		if m == nil && current != nil && g == current.Generation {
			m = current
		}
		if m == nil {
			fetched, err := rm.getManifest(ctx, manifestKey(g))
			if err != nil {
				fmt.Fprintf(out, "backup: cannot read kept generation %s (%v); leaving drops pending\n", g, err)
				return drop
			}
			m = fetched
			state.Manifests[g] = m
		}
		for _, e := range m.Files {
			refs[e.Object] = true
		}
	}
	for _, g := range drop {
		m := state.Manifests[g]
		if m == nil {
			fetched, err := rm.getManifest(ctx, manifestKey(g))
			if errors.Is(err, s3.ErrNotFound) {
				delete(state.Manifests, g)
				sum.Dropped = append(sum.Dropped, g)
				continue // already gone: done
			}
			if err != nil {
				fmt.Fprintf(out, "backup: drop %s: %v\n", g, err)
				remaining = append(remaining, g)
				continue
			}
			m = fetched
		}
		failed := false
		for _, e := range m.Files {
			if refs[e.Object] {
				continue
			}
			if err := rm.c.Delete(ctx, e.Object); err != nil {
				failed = true
				fmt.Fprintf(out, "backup: delete %s: %v\n", e.Object, err)
				continue
			}
			sum.Deleted++
		}
		if failed {
			remaining = append(remaining, g)
			continue
		}
		if err := rm.c.Delete(ctx, manifestKey(g)); err != nil {
			fmt.Fprintf(out, "backup: delete %s: %v\n", manifestKey(g), err)
			remaining = append(remaining, g)
			continue
		}
		delete(state.Manifests, g)
		sum.Dropped = append(sum.Dropped, g)
	}
	return remaining
}
