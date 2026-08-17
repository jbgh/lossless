package write

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const maxCatchUpBytes = 64 << 20

// checkIngestFile is the catch-up read gate. The daemon must not ingest
// arbitrary local files (/etc/passwd, ~/.ssh/id_rsa) just because a
// loopback client sent a path.
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
