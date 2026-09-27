package inspect

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"lossless/internal/store"
	"lossless/internal/write"
)

// SealCandidate is one plain tape the sweep would seal.
type SealCandidate struct {
	Path    string `json:"path"`
	Bytes   int64  `json:"bytes"`
	IdleFor string `json:"idle_for"`
	Sealed  bool   `json:"sealed"`
	Saved   int64  `json:"saved,omitempty"`
	Refused string `json:"refused,omitempty"`
	Err     string `json:"error,omitempty"`
	// Kept is set for a tape deliberately left plain, e.g. the current
	// month's manual/remember.jsonl.
	Kept string `json:"kept,omitempty"`
}

// SealSweep is the report for one pass over raw/.
type SealSweep struct {
	Idle       time.Duration   `json:"idle"`
	DryRun     bool            `json:"dry_run"`
	Tapes      int             `json:"tapes"`
	Bytes      int64           `json:"bytes"`
	BytesSaved int64           `json:"bytes_saved"`
	Sealed     int             `json:"sealed"`
	Kept       int             `json:"kept"`
	Candidates []SealCandidate `json:"candidates"`
}

// SealRawSweep walks raw/ and compresses the plain tapes that have been
// idle at least idle. It runs in the daemon so it shares the process
// with the watcher and the appends, and it seals through the same
// write.SealRaw everything else uses: verified round-trip, no clobber,
// and delete only after a verified copy.
//
// The current month's manual/remember.jsonl is the open append target
// and is always skipped. Nothing is ever deleted here: a refused seal
// and a fresh tape are both reported and left alone.
func SealRawSweep(st *store.Store, idle time.Duration, apply bool) (SealSweep, error) {
	return SealRawSweepCtx(context.Background(), st, idle, apply)
}

// SealRawSweepCtx is SealRawSweep that stops between tapes once ctx is
// done, so a daemon shutting down does not keep sealing after its
// watcher has returned. A tape already being sealed finishes: SealRaw
// leaves the plaintext in place until its verified copy is renamed.
func SealRawSweepCtx(ctx context.Context, st *store.Store, idle time.Duration, apply bool) (SealSweep, error) {
	var out SealSweep
	if idle <= 0 {
		idle = 24 * time.Hour
	}
	out.Idle = idle
	out.DryRun = !apply

	rawRoot := filepath.Join(st.Root, "raw")
	keep := st.ManualRawPath(time.Now())
	cutoff := time.Now().Add(-idle)

	err := filepath.WalkDir(rawRoot, func(path string, d fs.DirEntry, err error) error {
		if ctx.Err() != nil {
			return filepath.SkipAll
		}
		if err != nil {
			return nil
		}
		if d.IsDir() || !isPlainTape(d.Name()) {
			return nil
		}
		if path == keep {
			out.Kept++
			out.Candidates = append(out.Candidates, SealCandidate{
				Path: path, Kept: "current month manual tape is the open append target",
			})
			return nil
		}
		fi, err := d.Info()
		if err != nil {
			return nil
		}
		if fi.ModTime().After(cutoff) {
			return nil
		}
		cand := SealCandidate{
			Path:    path,
			Bytes:   fi.Size(),
			IdleFor: time.Since(fi.ModTime()).Truncate(time.Minute).String(),
		}
		out.Tapes++
		out.Bytes += cand.Bytes
		if !apply {
			out.Candidates = append(out.Candidates, cand)
			return nil
		}
		zst, err := write.SealRaw(path)
		switch {
		case err == nil:
			cand.Sealed = true
			if zi, serr := os.Stat(zst); serr == nil {
				cand.Saved = fi.Size() - zi.Size()
				out.BytesSaved += cand.Saved
			}
			out.Sealed++
		case errors.Is(err, write.ErrAlreadySealed):
			cand.Refused = "a .zst sibling already exists"
		default:
			cand.Err = err.Error()
		}
		out.Candidates = append(out.Candidates, cand)
		return nil
	})
	if err != nil {
		return out, err
	}
	return out, nil
}

// isPlainTape keeps the sweep to uncompressed .jsonl tapes: the .zst
// itself, the .lock sidecars, and a .tmp left by an interrupted seal.
func isPlainTape(name string) bool {
	return strings.HasSuffix(name, ".jsonl")
}

// FormatSealSweep prints the sweep report. It reports tapes and bytes;
// a dry run never compresses anything.
func FormatSealSweep(w io.Writer, s SealSweep) {
	mode := "dry run (nothing sealed)"
	if !s.DryRun {
		mode = "applied"
	}
	fmt.Fprintf(w, "\nseal-raw  idle >= %s  %s\n", s.Idle, mode)
	fmt.Fprintf(w, "  tapes %d  plain %s  saved %s  sealed %d  kept %d\n",
		s.Tapes, byteSize(s.Bytes), byteSize(s.BytesSaved), s.Sealed, s.Kept)
	for _, c := range s.Candidates {
		switch {
		case c.Kept != "":
			fmt.Fprintf(w, "  keep   %-52s %s\n", clip(c.Path, 52), c.Kept)
		case c.Sealed:
			fmt.Fprintf(w, "  sealed %-52s %s -> %s (saved %s)\n",
				clip(c.Path, 52), byteSize(c.Bytes), byteSize(c.Bytes-c.Saved), byteSize(c.Saved))
		case c.Refused != "":
			fmt.Fprintf(w, "  kept   %-52s %s\n", clip(c.Path, 52), c.Refused)
		case c.Err != "":
			fmt.Fprintf(w, "  error  %-52s %s\n", clip(c.Path, 52), c.Err)
		default:
			fmt.Fprintf(w, "  seal   %-52s %s (idle %s)\n", clip(c.Path, 52), byteSize(c.Bytes), c.IdleFor)
		}
	}
}
