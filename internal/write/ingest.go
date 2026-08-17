package write

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

const maxCatchUpBytes = 64 << 20

// CheckJSONLFile is the catch-up / inspect read gate. The daemon must
// not ingest arbitrary local files (/etc/passwd, ~/.ssh/id_rsa) just
// because a loopback client sent a path.
func CheckJSONLFile(path string) error {
	return checkIngestFile(path)
}

func checkIngestFile(path string) error {
	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("path required")
	}
	if strings.ContainsRune(path, 0) {
		return fmt.Errorf("invalid path")
	}
	if !strings.HasSuffix(strings.ToLower(path), ".jsonl") {
		return fmt.Errorf("ingest path must be a .jsonl file")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	fi, err := os.Lstat(abs)
	if err != nil {
		return err
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("ingest path must not be a symlink")
	}
	if !fi.Mode().IsRegular() {
		return fmt.Errorf("ingest path is not a regular file")
	}
	if fi.Size() > maxCatchUpBytes {
		return fmt.Errorf("ingest file too large (%d bytes)", fi.Size())
	}
	return nil
}

// openIngestFile Lstats then opens with O_NOFOLLOW so a symlink cannot
// be swapped in between the check and the read.
func openIngestFile(path string) (*os.File, os.FileInfo, error) {
	if err := checkIngestFile(path); err != nil {
		return nil, nil, err
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, nil, err
	}
	f, err := os.OpenFile(abs, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, nil, err
	}
	fi, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, nil, err
	}
	if !fi.Mode().IsRegular() {
		_ = f.Close()
		return nil, nil, fmt.Errorf("ingest path is not a regular file")
	}
	if fi.Size() > maxCatchUpBytes {
		_ = f.Close()
		return nil, nil, fmt.Errorf("ingest file too large (%d bytes)", fi.Size())
	}
	return f, fi, nil
}

func readCatchUpBytes(src *os.File, from int64, size int64) ([]byte, error) {
	remain := size - from
	if remain < 0 {
		remain = 0
	}
	if remain > maxCatchUpBytes {
		remain = maxCatchUpBytes
	}
	return io.ReadAll(io.LimitReader(src, remain))
}
