package harness

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"
)

func writeUserConfig(path string, body []byte, createMode os.FileMode) error {
	if createMode == 0 {
		createMode = 0o600
	}
	mode := createMode
	if fi, err := os.Lstat(path); err == nil {
		if fi.IsDir() {
			return fmt.Errorf("refusing to overwrite directory %s", path)
		}
		if fi.Mode()&os.ModeSymlink == 0 {
			if p := fi.Mode().Perm(); p != 0 {
				mode = p
			}
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, body, mode); err != nil {
		return err
	}
	if err := os.Chmod(tmp, mode); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

func checkUnitString(s, name string) error {
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			return fmt.Errorf("invalid %s (control characters)", name)
		}
	}
	return nil
}

func tomlTableName(line string) (string, bool) {
	trim := strings.TrimSpace(line)
	if !strings.HasPrefix(trim, "[") {
		return "", false
	}
	if i := strings.IndexByte(trim, '#'); i >= 0 {
		trim = strings.TrimSpace(trim[:i])
	}
	if !strings.HasSuffix(trim, "]") || len(trim) < 2 {
		return "", false
	}
	name := strings.TrimSpace(trim[1 : len(trim)-1])
	if name == "" || strings.ContainsFunc(name, unicode.IsSpace) {
		return "", false
	}
	return name, true
}
