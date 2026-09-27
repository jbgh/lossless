package write

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/klauspost/compress/zstd"
)

// ErrAlreadySealed: a .zst sibling already exists. Sealing refuses
// rather than overwrites it. manual/<month>/remember.jsonl has no
// .partN rollover, so the second seal of a month would replace the
// .zst the first one wrote and lose everything sealed before it.
var ErrAlreadySealed = errors.New("sealed tape already exists")

// IsAlreadySealed reports a refusal to clobber an earlier .zst, so a
// caller (the sweep) can count it as kept rather than as a failure.
func IsAlreadySealed(err error) bool {
	return errors.Is(err, ErrAlreadySealed)
}

// SealRaw compresses an uncompressed raw JSONL next to itself and removes
// the plaintext. Idempotent if already sealed.
//
// The plaintext is unlinked only after the compressed copy has been
// verified to decompress to the same SHA-256, renamed into place, and
// the directory fsynced. The whole copy-verify-unlink runs under the
// same flock the append path takes for one write, so a line appended
// mid-seal is not dropped on the floor.
func SealRaw(path string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("empty path")
	}
	if strings.HasSuffix(path, ".zst") {
		if _, err := os.Stat(path); err != nil {
			return "", err
		}
		return path, nil
	}
	zst := path + ".zst"
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			// Already sealed: the plaintext is gone and its .zst is here.
			if _, e2 := os.Stat(zst); e2 == nil {
				return zst, nil
			}
		}
		return "", err
	}
	if _, err := os.Stat(zst); err == nil {
		// Sealed by an earlier pass. os.Rename would overwrite it.
		return "", fmt.Errorf("%s: %w", filepath.Base(zst), ErrAlreadySealed)
	} else if !os.IsNotExist(err) {
		return "", err
	}
	src, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer src.Close()
	// The append path holds this lock for one write. Holding it across
	// the copy keeps a concurrent append from landing in a file that is
	// about to be unlinked; a writer that opened the file before the
	// unlink re-checks its own link count once it gets the lock.
	if err := syscall.Flock(int(src.Fd()), syscall.LOCK_EX); err != nil {
		return "", err
	}
	defer func() { _ = syscall.Flock(int(src.Fd()), syscall.LOCK_UN) }()
	// Re-check under the lock: another pass may have sealed this tape
	// between the stat above and the lock.
	if _, err := os.Stat(zst); err == nil {
		return "", fmt.Errorf("%s: %w", filepath.Base(zst), ErrAlreadySealed)
	} else if !os.IsNotExist(err) {
		return "", err
	}
	tmp := zst + ".tmp"
	dst, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return "", err
	}
	enc, err := zstd.NewWriter(dst, zstd.WithEncoderLevel(zstd.SpeedDefault))
	if err != nil {
		_ = dst.Close()
		_ = os.Remove(tmp)
		return "", err
	}
	// Hash the plaintext as it streams, so the verify reads the same
	// bytes without a second pass over the source.
	plain := sha256.New()
	if _, err := io.Copy(enc, io.TeeReader(src, plain)); err != nil {
		_ = enc.Close()
		_ = dst.Close()
		_ = os.Remove(tmp)
		return "", err
	}
	if err := enc.Close(); err != nil {
		_ = dst.Close()
		_ = os.Remove(tmp)
		return "", err
	}
	if err := dst.Sync(); err != nil {
		_ = dst.Close()
		_ = os.Remove(tmp)
		return "", err
	}
	if err := dst.Close(); err != nil {
		_ = os.Remove(tmp)
		return "", err
	}
	// Delete never: the plaintext outlives every failure above. Only a
	// verified copy earns the unlink.
	if err := verifySeal(tmp, plain.Sum(nil)); err != nil {
		_ = os.Remove(tmp)
		return "", err
	}
	if err := os.Rename(tmp, zst); err != nil {
		_ = os.Remove(tmp)
		return "", err
	}
	if err := fsyncDir(filepath.Dir(path)); err != nil {
		return zst, err
	}
	if err := os.Remove(path); err != nil {
		return zst, err
	}
	return zst, nil
}

// verifySeal decompresses a freshly written .zst and compares its
// SHA-256 against the digest of the plaintext it was made from. A
// mismatch means the copy is not the tape, and the caller keeps both.
func verifySeal(zst string, want []byte) error {
	f, err := os.Open(zst)
	if err != nil {
		return err
	}
	defer f.Close()
	dec, err := zstd.NewReader(f)
	if err != nil {
		return err
	}
	defer dec.Close()
	got := sha256.New()
	if _, err := io.Copy(got, dec); err != nil {
		return err
	}
	if !bytes.Equal(got.Sum(nil), want) {
		return fmt.Errorf("seal verify: %s does not decompress to its plaintext", filepath.Base(zst))
	}
	return nil
}

// fsyncDir makes a rename or an unlink durable, not just visible.
func fsyncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}

func ReadRaw(path string) ([]byte, error) {
	if _, err := os.Stat(path); err == nil {
		f, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		return io.ReadAll(io.LimitReader(f, maxCatchUpBytes))
	}
	zst := path
	if !strings.HasSuffix(path, ".zst") {
		zst = path + ".zst"
	}
	f, err := os.Open(zst)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	dec, err := zstd.NewReader(f)
	if err != nil {
		return nil, err
	}
	defer dec.Close()
	return io.ReadAll(io.LimitReader(dec, maxCatchUpBytes))
}
