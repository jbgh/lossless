package write

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/klauspost/compress/zstd"
)

// SealRaw compresses an uncompressed raw JSONL next to itself and removes
// the plaintext. Idempotent if already sealed.
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
			if _, e2 := os.Stat(zst); e2 == nil {
				return zst, nil
			}
		}
		return "", err
	}
	src, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer src.Close()
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
	if _, err := io.Copy(enc, src); err != nil {
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
	if err := os.Rename(tmp, zst); err != nil {
		_ = os.Remove(tmp)
		return "", err
	}
	if err := os.Remove(path); err != nil {
		return zst, err
	}
	return zst, nil
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
