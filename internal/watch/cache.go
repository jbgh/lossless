package watch

import (
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"lossless/internal/harness"
	"lossless/internal/store"
	"lossless/internal/write"
)

// Watcher holds what one watch loop learns between ticks so an idle tick
// costs a stat per file, not an open and a parse. 0.1.42 walked every
// root and peeked every transcript's head on every 1s tick: 2,400 opens
// and JSON parses a second on a machine with 9.8 GB of Claude projects,
// half a core around the clock.
//
// Everything cached is a shortcut to an answer a fresh pass would give:
//   - the session table, kept while the store's session generation holds
//     (a row another process writes is seen at the verify pass);
//   - a transcript's peeked cwd, kept while the file's size and mtime
//     hold (and for good once found: a session's cwd never changes);
//   - a directory's listing, kept while its mtime holds;
//   - the OpenCode session list, kept while opencode.db and its WAL hold;
//   - the Codex state threads, kept while each state database and its WAL
//     hold (whether a thread's rollout file exists is checked every tick);
//   - "this target was caught up and checked for an idle seal", kept
//     while its file's size and mtime (or its harness update time) hold.
//
// A listing or a stat younger than racyWindow is not trusted (a coarse
// clock can stamp two writes alike), and every verifyEvery the walk and
// the per-target checks run in full.
//
// A Watcher is used by one goroutine. The zero value is not ready; use
// NewWatcher.
type Watcher struct {
	gen        uint64
	verify     bool // this tick runs in full
	lastVerify time.Time

	peeks map[string]peekEntry
	dirs  map[string]dirEntry
	quiet map[string]quietEntry

	ocSig  [2]fileSig
	ocList []write.OpenCodeSession
	ocOK   bool

	// The session table, kept while the store's SessionsGen holds.
	known         []store.Session
	knownProjects map[string]string
	knownStore    *store.Store
	knownGen      uint64
	knownOK       bool

	codexHome string
	codexSig  []fileSig
	codexRows []harness.CodexThread
	codexOK   bool
}

// verifyEvery is how often a tick ignores the listing and per-target
// caches. An idle seal waits at most this long past its 24h threshold.
const verifyEvery = 5 * time.Minute

// racyWindow: a directory or file whose mtime is this close to now may
// still change without its mtime moving on a filesystem with a coarse
// clock, so its cached state is not trusted.
const racyWindow = 2 * time.Second

func NewWatcher() *Watcher {
	return &Watcher{
		peeks: map[string]peekEntry{},
		dirs:  map[string]dirEntry{},
		quiet: map[string]quietEntry{},
	}
}

type fileSig struct {
	ok   bool
	size int64
	mod  int64 // UnixNano
}

func sigOf(fi fs.FileInfo) fileSig {
	return fileSig{ok: true, size: fi.Size(), mod: fi.ModTime().UnixNano()}
}

func lstatSig(path string) fileSig {
	fi, err := os.Lstat(path)
	if err != nil {
		return fileSig{}
	}
	return sigOf(fi)
}

func racy(fi fs.FileInfo, now time.Time) bool {
	return now.Sub(fi.ModTime()) < racyWindow
}

type peekEntry struct {
	sig fileSig
	cwd string
	gen uint64
}

type dirEntry struct {
	mod     int64
	entries []fs.DirEntry
	gen     uint64
}

type quietEntry struct {
	sig     fileSig
	updated int64
	gen     uint64
}

// beginTick starts a generation. A verify tick drops the listing and
// per-target caches. Found cwds stay (they do not change); empty ones are
// dropped too, so a peek that failed for a passing reason (EMFILE, a
// permission flip) on a file that never changes is retried here.
func (w *Watcher) beginTick(now time.Time) {
	w.gen++
	w.verify = w.lastVerify.IsZero() || now.Sub(w.lastVerify) >= verifyEvery
	if w.verify {
		w.lastVerify = now
		w.dirs = map[string]dirEntry{}
		w.quiet = map[string]quietEntry{}
		w.ocOK = false
		w.codexOK = false
		w.knownOK = false
		for k, e := range w.peeks {
			if e.cwd == "" {
				delete(w.peeks, k)
			}
		}
	}
}

// endTick drops cache entries for files, directories and targets this
// pass did not reach: they are gone, or no longer under a root.
func (w *Watcher) endTick() {
	for k, e := range w.peeks {
		if e.gen != w.gen {
			delete(w.peeks, k)
		}
	}
	for k, e := range w.dirs {
		if e.gen != w.gen {
			delete(w.dirs, k)
		}
	}
	for k, e := range w.quiet {
		if e.gen != w.gen {
			delete(w.quiet, k)
		}
	}
}

// walk visits every non-directory entry under root in lexical order, like
// filepath.WalkDir with a callback that ignores directories and errors:
// root itself is Lstat'ed (a symlinked root is not followed), symlinked
// directories are not entered, and an unreadable directory is skipped.
// A directory whose mtime has not moved since it was last read is not
// read again; it still costs one Lstat. Appending to a file does not move
// its directory's mtime, so callers stat the files they care about.
func (w *Watcher) walk(root string, fn func(path string, d fs.DirEntry)) {
	fi, err := os.Lstat(root)
	if err != nil {
		return
	}
	if !fi.IsDir() {
		fn(root, fs.FileInfoToDirEntry(fi))
		return
	}
	w.walkDir(root, fi, time.Now(), fn)
}

func (w *Watcher) walkDir(dir string, fi fs.FileInfo, now time.Time, fn func(string, fs.DirEntry)) {
	mod := fi.ModTime().UnixNano()
	var entries []fs.DirEntry
	if e, ok := w.dirs[dir]; ok && e.mod == mod {
		entries = e.entries
		e.gen = w.gen
		w.dirs[dir] = e
	} else {
		// Stat before reading: an entry added after the read moves the
		// mtime past the one cached here.
		var err error
		entries, err = os.ReadDir(dir)
		if err == nil && !racy(fi, now) {
			w.dirs[dir] = dirEntry{mod: mod, entries: entries, gen: w.gen}
		} else {
			delete(w.dirs, dir)
		}
	}
	for _, e := range entries {
		p := filepath.Join(dir, e.Name())
		if e.IsDir() {
			sub, err := os.Lstat(p)
			if err != nil || !sub.IsDir() {
				continue
			}
			w.walkDir(p, sub, now, fn)
			continue
		}
		fn(p, e)
	}
}

// peek returns a transcript's cwd through the cache. A cwd once found is
// kept while the file exists; an empty one is peeked again only when the
// file's size or mtime moves (a new session writes its cwd line later).
func (w *Watcher) peek(path string, d fs.DirEntry, fn func(string) string) string {
	// The walk listed the file, so it exists; a found cwd needs no stat.
	if e, ok := w.peeks[path]; ok && e.cwd != "" && d != nil {
		e.gen = w.gen
		w.peeks[path] = e
		return e.cwd
	}
	var sig fileSig
	if d != nil {
		if fi, err := d.Info(); err == nil {
			sig = sigOf(fi)
		}
	} else {
		sig = lstatSig(path)
	}
	if !sig.ok {
		delete(w.peeks, path)
		return fn(path)
	}
	if e, ok := w.peeks[path]; ok && (e.sig == sig || e.cwd != "") {
		e.gen = w.gen
		w.peeks[path] = e
		return e.cwd
	}
	cwd := fn(path)
	w.peeks[path] = peekEntry{sig: sig, cwd: cwd, gen: w.gen}
	return cwd
}

// openCodeSessions lists the OpenCode database's sessions, reusing the
// last list while opencode.db and its WAL are unchanged.
func (w *Watcher) openCodeSessions(db string) []write.OpenCodeSession {
	now := time.Now()
	var sig [2]fileSig
	trust := true
	for i, p := range []string{db, db + "-wal"} {
		fi, err := os.Lstat(p)
		if err != nil {
			continue
		}
		sig[i] = sigOf(fi)
		if racy(fi, now) {
			trust = false
		}
	}
	if w.ocOK && sig == w.ocSig {
		return w.ocList
	}
	list := write.ListOpenCodeSessions(db)
	w.ocOK = trust && sig[0].ok
	w.ocSig, w.ocList = sig, list
	return list
}

// knownSessions returns the session table and the project of each session
// by harness and id, reading the table again only when this store changed
// it since the last read, or at the verify pass. The generation is read
// before the rows, so a write landing during the read moves it past the
// one kept here and the next tick reads again.
func (w *Watcher) knownSessions(st *store.Store) ([]store.Session, map[string]string, error) {
	gen := st.SessionsGen()
	if w.knownOK && w.knownStore == st && w.knownGen == gen {
		return w.known, w.knownProjects, nil
	}
	known, err := listSessions(st)
	if err != nil {
		w.knownOK = false
		return nil, nil, err
	}
	w.known, w.knownProjects = known, projectsBySession(known)
	w.knownStore, w.knownGen, w.knownOK = st, gen, true
	return known, w.knownProjects, nil
}

// codexThreads lists the Codex state threads, reusing the rows read last
// time while every state database and its WAL are unchanged. The rollout
// file check runs every tick: a rollout written after its thread row
// becomes a file target without a database change.
func (w *Watcher) codexThreads(home string) []harness.CodexThread {
	now := time.Now()
	var sig []fileSig
	trust := true
	for _, db := range harness.CodexStateDBs(home) {
		for _, p := range []string{db, db + "-wal"} {
			fi, err := os.Lstat(p)
			if err != nil {
				sig = append(sig, fileSig{})
				continue
			}
			sig = append(sig, sigOf(fi))
			if racy(fi, now) {
				trust = false
			}
		}
	}
	if !w.codexOK || w.codexHome != home || !sameSigs(sig, w.codexSig) {
		w.codexRows = codexStateRows(home)
		w.codexHome, w.codexSig = home, sig
		w.codexOK = trust
	}
	return harness.CodexLiveRollouts(w.codexRows)
}

func sameSigs(a, b []fileSig) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// isQuiet reports a target this watcher already found caught up, with
// its idle seal checked, at the same file state (or harness update time).
func (w *Watcher) isQuiet(key string, sig fileSig, updated int64) bool {
	if w.verify || key == "" {
		return false
	}
	e, ok := w.quiet[key]
	if !ok || e.sig != sig || e.updated != updated {
		return false
	}
	e.gen = w.gen
	w.quiet[key] = e
	return true
}

func (w *Watcher) markQuiet(key string, sig fileSig, updated int64) {
	if key == "" {
		return
	}
	w.quiet[key] = quietEntry{sig: sig, updated: updated, gen: w.gen}
}
