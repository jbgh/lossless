package projectkey

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

var looseOrigin = regexp.MustCompile(`[:/]([A-Za-z0-9_.-]+)/([A-Za-z0-9_.-]+?)(?:\.git)?$`)

func Decode(enc string) string {
	enc = strings.TrimSpace(enc)
	if enc == "" {
		return ""
	}
	return Normalize(strings.Replace(enc, "__", "/", 1))
}

func Encode(key string) string {
	s := strings.ReplaceAll(Normalize(key), "/", "__")
	s = strings.ReplaceAll(s, string(filepath.Separator), "__")
	s = strings.Trim(s, ". ")
	if s == "" || s == "." || s == ".." || strings.Contains(s, "..") {
		return "unknown"
	}
	return s
}

func Normalize(input string) string {
	s := strings.ToLower(strings.TrimSpace(input))
	s = strings.TrimSuffix(s, ".git")
	if strings.Contains(s, "/") {
		if k := FromOrigin(s); k != "" {
			return k
		}
		parts := strings.Split(s, "/")
		var keep []string
		for _, p := range parts {
			if p != "" {
				keep = append(keep, p)
			}
		}
		if len(keep) >= 2 {
			return keep[len(keep)-2] + "/" + keep[len(keep)-1]
		}
	}
	if strings.Contains(s, "__") {
		return strings.Replace(s, "__", "/", 1)
	}
	return s
}

func FromOrigin(origin string) string {
	raw := strings.TrimSpace(origin)
	raw = strings.TrimPrefix(raw, "git+")
	if m := looseOrigin.FindStringSubmatch(raw); len(m) == 3 {
		repo := strings.TrimSuffix(strings.ToLower(m[2]), ".git")
		return strings.ToLower(m[1]) + "/" + repo
	}
	return ""
}

func FromWorkspace(root string) string {
	abs, err := filepath.Abs(root)
	if err != nil {
		abs = root
	}
	if o := gitOutput(abs, "remote", "get-url", "origin"); o != "" {
		if k := FromOrigin(o); k != "" {
			return k
		}
	}
	top := gitOutput(abs, "rev-parse", "--show-toplevel")
	if top == "" {
		top = abs
	}
	if r, err := filepath.EvalSymlinks(top); err == nil {
		top = r
	}
	sum := sha256.Sum256([]byte(top))
	return "path-" + hex.EncodeToString(sum[:])[:16]
}

func gitOutput(cwd string, args ...string) string {
	cmd := exec.Command("git", append([]string{"-C", cwd}, args...)...)
	cmd.Env = os.Environ()
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
