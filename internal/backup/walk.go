// internal/backup/walk.go
package backup

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"lossless/internal/backup/crypt"
	"lossless/internal/store"
)

// Item is one file the backup wants in the bucket.
type Item struct {
	Rel      string // relative to home, forward slashes
	Src      string // path to read: the original, or a copy under backup-tmp
	Size     int64  // bytes that go in the manifest
	StatSize int64  // on-disk size when hashed; sqlite: main + -wal
	Mtime    int64  // cache key of the original (ns); sqlite: max of main and -wal
	SHA256   string
	Object   string // o/<name>/<sha256>
	Temp     bool   // Src is under backup-tmp
}

var roots = []string{"raw", "export", "index"}

// validRel reports whether rel is a safe path to restore: relative, with no
// empty or ".." segment, and under one of the three roots the walk emits.
// restore uses this to reject a manifest that names a path outside home.
func validRel(rel string) bool {
	if rel == "" || filepath.IsAbs(rel) || strings.ContainsRune(rel, '\\') {
		return false
	}
	inRoot := false
	for _, r := range roots {
		if rel == r || strings.HasPrefix(rel, r+"/") {
			inRoot = true
			break
		}
	}
	if !inRoot {
		return false
	}
	for _, seg := range strings.Split(rel, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return false
		}
	}
	return true
}

func skipName(name string) bool {
	for _, suf := range []string{".lock", ".tmp", "-wal", "-shm"} {
		if strings.HasSuffix(name, suf) {
			return true
		}
	}
	return false
}

// walk lists everything under raw/, export/, and index/ that belongs in a
// backup. Live parts and sqlite files are copied to tmp first; sealed parts
// and claim files are read in place. cache short-circuits hashing when size
// and mtime match, but for live and sqlite files only when pointer already
// holds that hash: otherwise anything about to be uploaded must be a fresh
// locked copy or snapshot, never the live original (see uploadOne).
func walk(home, tmp string, keys *crypt.Keys, cache *State, pointer *Manifest) ([]Item, error) {
	if err := os.MkdirAll(tmp, 0o700); err != nil {
		return nil, err
	}
	var items []Item
	for _, root := range roots {
		dir := filepath.Join(home, root)
		if _, err := os.Stat(dir); err != nil {
			continue
		}
		err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || d.Type()&fs.ModeSymlink != 0 || skipName(d.Name()) {
				return nil
			}
			rel, err := filepath.Rel(home, path)
			if err != nil {
				return err
			}
			rel = filepath.ToSlash(rel)
			var it Item
			switch {
			case root == "raw" && strings.HasSuffix(rel, ".jsonl.zst"):
				it, err = directItem(home, rel, keys, cache)
			case root == "raw" && strings.HasSuffix(rel, ".jsonl"):
				it, err = liveItem(home, tmp, rel, keys, cache, pointer)
			case root == "export" && strings.HasSuffix(rel, ".md"):
				it, err = directItem(home, rel, keys, cache)
			case root == "index" && (d.Name() == "claims.sqlite" ||
				(strings.HasPrefix(d.Name(), "excerpts-") && strings.HasSuffix(d.Name(), ".sqlite"))):
				it, err = sqliteItem(home, tmp, rel, keys, cache, pointer)
			default:
				return nil
			}
			if err != nil {
				return fmt.Errorf("%s: %w", rel, err)
			}
			if it.Rel != "" {
				items = append(items, it)
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	return items, nil
}

func hashFile(path string) (string, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(h.Sum(nil)), n, nil
}

func finish(it Item, keys *crypt.Keys) Item {
	it.Object = objectKey(keys.Name(it.Rel), it.SHA256)
	return it
}

// directItem reads an immutable or atomically-replaced file in place.
func directItem(home, rel string, keys *crypt.Keys, cache *State) (Item, error) {
	path := filepath.Join(home, filepath.FromSlash(rel))
	st, err := os.Stat(path)
	if err != nil {
		return Item{}, err
	}
	it := Item{Rel: rel, Src: path, Size: st.Size(), StatSize: st.Size(), Mtime: st.ModTime().UnixNano()}
	if c, ok := cache.Files[rel]; ok && c.StatSize == it.StatSize && c.Mtime == it.Mtime && c.SHA256 != "" {
		it.SHA256 = c.SHA256
		return finish(it, keys), nil
	}
	sha, n, err := hashFile(path)
	if err != nil {
		return Item{}, err
	}
	it.SHA256, it.Size = sha, n
	return finish(it, keys), nil
}

// liveItem copies a live part to tmp under shared flocks on the part and
// its .lock sidecar, then hashes the copy. If the part was sealed between
// the walk and the open, the .zst sibling is used in place instead.
func liveItem(home, tmp, rel string, keys *crypt.Keys, cache *State, pointer *Manifest) (Item, error) {
	path := filepath.Join(home, filepath.FromSlash(rel))
	st, err := os.Stat(path)
	if os.IsNotExist(err) {
		if _, zerr := os.Stat(path + ".zst"); zerr == nil {
			return directItem(home, rel+".zst", keys, cache)
		}
		return Item{}, nil
	}
	if err != nil {
		return Item{}, err
	}
	it := Item{Rel: rel, Size: st.Size(), StatSize: st.Size(), Mtime: st.ModTime().UnixNano()}
	if c, ok := cache.Files[rel]; ok && c.StatSize == it.StatSize && c.Mtime == it.Mtime && c.SHA256 != "" && pointer.Files[rel].SHA256 == c.SHA256 {
		// Unchanged since the last run and already the hash the bucket
		// pointer holds for this path: no copy is needed. If the pointer
		// lacks it (wiped, rolled back, a new prefix, --take-over from
		// another writer), fall through and copy like a cache miss so
		// nothing is ever uploaded straight from the live original.
		it.SHA256 = c.SHA256
		it.Src = path
		return finish(it, keys), nil
	}
	dst := filepath.Join(tmp, keys.Name(rel)+".live")
	if err := copyLive(path, dst); err != nil {
		if os.IsNotExist(err) {
			if _, zerr := os.Stat(path + ".zst"); zerr == nil {
				return directItem(home, rel+".zst", keys, cache)
			}
			return Item{}, nil
		}
		return Item{}, err
	}
	sha, n, err := hashFile(dst)
	if err != nil {
		return Item{}, err
	}
	it.Src, it.SHA256, it.Size, it.Temp = dst, sha, n, true
	return finish(it, keys), nil
}

// copyLive holds LOCK_SH on <part>.lock (catch-up's lock) and on the part
// itself (append's lock) only for the local copy. Both writers hold their
// exclusive lock for one append and fsync, so this waits microseconds and
// never makes a hook wait on the network.
func copyLive(src, dst string) error {
	lockFile, err := os.OpenFile(src+".lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	defer lockFile.Close()
	if err := syscall.Flock(int(lockFile.Fd()), syscall.LOCK_SH); err != nil {
		return err
	}
	defer func() { _ = syscall.Flock(int(lockFile.Fd()), syscall.LOCK_UN) }()
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := syscall.Flock(int(in.Fd()), syscall.LOCK_SH); err != nil {
		return err
	}
	defer func() { _ = syscall.Flock(int(in.Fd()), syscall.LOCK_UN) }()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}

// sqliteItem snapshots a database with VACUUM INTO. The cache key combines
// the main file and its -wal so an uncheckpointed write still re-snapshots.
func sqliteItem(home, tmp, rel string, keys *crypt.Keys, cache *State, pointer *Manifest) (Item, error) {
	path := filepath.Join(home, filepath.FromSlash(rel))
	st, err := os.Stat(path)
	if err != nil {
		return Item{}, err
	}
	if st.Size() == 0 {
		return Item{}, nil
	}
	size, mtime := st.Size(), st.ModTime().UnixNano()
	if wst, err := os.Stat(path + "-wal"); err == nil {
		size += wst.Size()
		if m := wst.ModTime().UnixNano(); m > mtime {
			mtime = m
		}
	}
	it := Item{Rel: rel, Mtime: mtime, StatSize: size}
	if c, ok := cache.Files[rel]; ok && c.Mtime == mtime && c.StatSize == size && c.SHA256 != "" && pointer.Files[rel].SHA256 == c.SHA256 {
		// Same guard as liveItem: only skip the VACUUM INTO snapshot when
		// the bucket already holds this exact hash. Reading the raw main
		// file instead would miss rows still sitting in the WAL.
		it.SHA256, it.Size, it.Src = c.SHA256, c.Size, path
		return finish(it, keys), nil
	}
	dst := filepath.Join(tmp, keys.Name(rel)+".sqlite")
	if err := store.Snapshot(path, dst); err != nil {
		return Item{}, err
	}
	sha, n, err := hashFile(dst)
	if err != nil {
		return Item{}, err
	}
	it.Src, it.SHA256, it.Size, it.Temp = dst, sha, n, true
	it.Mtime = mtime
	return finish(it, keys), nil
}
