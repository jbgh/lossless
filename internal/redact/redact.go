package redact

import (
	"encoding/json"
	"regexp"
	"strings"
)

var secrets = []*regexp.Regexp{
	regexp.MustCompile(`AKIA[0-9A-Z]{16}`),
	regexp.MustCompile(`-----BEGIN [A-Z ]+PRIVATE KEY-----`),
	regexp.MustCompile(`Bearer [A-Za-z0-9._\-]{20,}`),
	regexp.MustCompile(`\bghp_[A-Za-z0-9]{20,}`),
	regexp.MustCompile(`\bgithub_pat_[A-Za-z0-9_]{20,}`),
	regexp.MustCompile(`\bsk-[A-Za-z0-9]{20,}`),
	regexp.MustCompile(`\bsk-ant-[A-Za-z0-9\-_]{20,}`),
	regexp.MustCompile(`\bsk-proj-[A-Za-z0-9_-]{20,}`),
	regexp.MustCompile(`\bsk_(?:live|test)_[A-Za-z0-9]{20,}`),
	regexp.MustCompile(`\bxai-[A-Za-z0-9_-]{20,}`),
	regexp.MustCompile(`\bAIza[0-9A-Za-z_-]{30,}`),
	regexp.MustCompile(`\bxox[baprs]-[A-Za-z0-9-]{10,}`),
	regexp.MustCompile(`\bnpm_[A-Za-z0-9]{20,}`),
	regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}`),
	regexp.MustCompile(`(?i)\b(?:postgres(?:ql)?|mysql|mongodb(?:\+srv)?)://[^:\s]+:[^@\s]+@`),
}

var sensitivePath = regexp.MustCompile(`(?:^|/)(?:\.env(?:\..+)?|\.envrc|.*\.pem|id_rsa|id_rsa\.pub|id_ed25519|id_ed25519\.pub|id_ecdsa|id_dsa|authorized_keys|credentials(?:\.json)?|aws-exports\.js)$`)

func ContainsSecret(text string) bool {
	for _, re := range secrets {
		if re.MatchString(text) {
			return true
		}
	}
	return false
}

func ShouldDropClaim(text string, paths []string) bool {
	if ContainsSecret(text) {
		return true
	}
	for _, p := range paths {
		if sensitivePath.MatchString(p) && (strings.Contains(text, "=") || strings.Contains(strings.ToUpper(text), "SECRET")) {
			return true
		}
	}
	return false
}

func FilterPaths(paths []string) []string {
	var out []string
	for _, p := range paths {
		p = normalizeRelPath(p)
		if p == "" || traversalPath(p) || remotePath(p) || gitDirPath(p) || depDirPath(p) || sensitivePath.MatchString(p) {
			continue
		}
		out = append(out, p)
	}
	return out
}

func normalizeRelPath(p string) string {
	p = strings.TrimSpace(p)
	p = strings.ReplaceAll(p, "\\", "/")
	for strings.HasPrefix(p, "./") {
		p = strings.TrimPrefix(p, "./")
	}
	p = strings.TrimSuffix(p, "/")
	if p == "." || p == ".." {
		return ""
	}
	return p
}

func traversalPath(p string) bool {
	p = strings.TrimSpace(p)
	if p == "" {
		return true
	}
	n := strings.ReplaceAll(p, "\\", "/")
	if strings.Contains(n, "\x00") {
		return true
	}
	low := strings.ToLower(n)
	if strings.Contains(low, "%2e") || strings.Contains(low, "%2f") || strings.Contains(low, "%00") {
		return true
	}
	if strings.HasPrefix(n, "/") || strings.HasPrefix(n, "~") {
		return true
	}
	if len(n) > 1 && n[1] == ':' {
		return true
	}
	parts := strings.Split(n, "/")
	if strippedAbs(parts[0]) {
		return true
	}
	for _, part := range parts {
		if part == ".." {
			return true
		}
	}
	return false
}

func gitDirPath(p string) bool {
	n := strings.ReplaceAll(strings.TrimSpace(p), "\\", "/")
	for _, part := range strings.Split(n, "/") {
		if part == ".git" {
			return true
		}
	}
	return false
}

func depDirPath(p string) bool {
	n := strings.ReplaceAll(strings.TrimSpace(p), "\\", "/")
	for _, part := range strings.Split(n, "/") {
		switch part {
		case "node_modules", "bower_components", "dist", ".next", "coverage", "__pycache__",
			"target", "Pods", "Carthage", "DerivedData", ".venv", "venv", "site-packages":
			return true
		}
	}
	return false
}

func strippedAbs(first string) bool {
	switch strings.ToLower(first) {
	case "users", "home", "etc", "var", "tmp", "private", "root", "windows", "proc", "dev":
		return true
	}
	return false
}

func remotePath(p string) bool {
	n := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(p), "\\", "/"))
	if strings.Contains(n, "://") || strings.HasSuffix(n, ".git") {
		return true
	}
	host := strings.Split(n, "/")[0]
	// Hidden dirs (.github) are not hosts. git.memora.pics is.
	if host == "" || strings.HasPrefix(host, ".") {
		return false
	}
	// LightboxView.swift is a file stem, not a host.
	if !strings.Contains(n, "/") && codeFileStem(host) {
		return false
	}
	return strings.Contains(host, ".")
}

func codeFileStem(p string) bool {
	i := strings.LastIndex(p, ".")
	if i <= 0 || i == len(p)-1 {
		return false
	}
	switch p[i+1:] {
	case "swift", "kt", "kts", "ts", "tsx", "js", "jsx", "mjs", "cjs",
		"go", "java", "m", "mm", "h", "hpp", "c", "cc", "cpp", "rs",
		"py", "rb", "php", "cs", "json", "toml", "yaml", "yml",
		"md", "proto", "sql", "gradle":
		return true
	default:
		return false
	}
}

// Line returns the line to append to raw. Secret lines become {"_redacted":true}.
func Line(line string) string {
	trim := strings.TrimSpace(line)
	if trim == "" {
		return line
	}
	if ContainsSecret(line) {
		b, _ := json.Marshal(map[string]bool{"_redacted": true})
		return string(b) + "\n"
	}
	if !strings.HasSuffix(line, "\n") {
		return line + "\n"
	}
	return line
}
