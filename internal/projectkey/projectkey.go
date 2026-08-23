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

// gitBinary is git on PATH, else a known absolute path. Launchd/systemd
// units may have no PATH; MCP ask still has to read origin.
func gitBinary() string {
	if p, err := exec.LookPath("git"); err == nil {
		return p
	}
	for _, p := range []string{"/usr/bin/git", "/opt/homebrew/bin/git", "/usr/local/bin/git"} {
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
			return p
		}
	}
	return ""
}

func gitOutput(cwd string, args ...string) string {
	bin := gitBinary()
	if bin == "" {
		return ""
	}
	cmd := exec.Command(bin, append([]string{"-C", cwd}, args...)...)
	cmd.Env = os.Environ()
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// Identity reports whether FromWorkspace can turn cwd into owner/repo
// when origin exists. Used by doctor so a PATH-less daemon is visible.
func Identity(cwd string) (ok bool, detail string) {
	if gitBinary() == "" {
		return false, "git not found"
	}
	if cwd == "" {
		var err error
		cwd, err = os.Getwd()
		if err != nil {
			return true, "git ok"
		}
	}
	origin := gitOutput(cwd, "remote", "get-url", "origin")
	if origin == "" {
		return true, "git ok; cwd has no origin"
	}
	want := FromOrigin(origin)
	if want == "" {
		return true, "git ok; origin not owner/repo"
	}
	got := FromWorkspace(cwd)
	if strings.HasPrefix(got, "path-") {
		return false, "origin is " + want + " but FromWorkspace is " + got
	}
	if got != want {
		return false, "FromWorkspace " + got + " != origin " + want
	}
	return true, got
}
