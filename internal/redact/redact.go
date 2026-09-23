package redact

import (
	"encoding/json"
	"regexp"
	"strings"
)

var secrets = []*regexp.Regexp{
	regexp.MustCompile(`\b(?:AKIA|ASIA)[0-9A-Z]{16}`),
	regexp.MustCompile(`-----BEGIN (?:[A-Z ]+ )?PRIVATE KEY-----`),
	regexp.MustCompile(`(?i)\bBearer [A-Za-z0-9._\-]{20,}`),
	regexp.MustCompile(`(?i)\bAuthorization:\s*Basic\s+[A-Za-z0-9+/=]{16,}`),
	regexp.MustCompile(`\bgh[opsu]_[A-Za-z0-9]{20,}`),
	regexp.MustCompile(`\bgithub_pat_[A-Za-z0-9_]{20,}`),
	regexp.MustCompile(`\bglpat-[A-Za-z0-9_\-]{20,}`),
	regexp.MustCompile(`\bsk-[A-Za-z0-9]{20,}`),
	regexp.MustCompile(`\bsk-ant-[A-Za-z0-9\-_]{20,}`),
	regexp.MustCompile(`\bsk-proj-[A-Za-z0-9_-]{20,}`),
	regexp.MustCompile(`\b[sr]k_(?:live|test)_[A-Za-z0-9]{20,}`),
	regexp.MustCompile(`\bxai-[A-Za-z0-9_-]{20,}`),
	regexp.MustCompile(`\bAIza[0-9A-Za-z_-]{30,}`),
	regexp.MustCompile(`\bxox[baprs]-[A-Za-z0-9-]{10,}`),
	regexp.MustCompile(`\bnpm_[A-Za-z0-9]{20,}`),
	regexp.MustCompile(`\bSG\.[A-Za-z0-9_-]{16,}\.[A-Za-z0-9_-]{16,}`),
	regexp.MustCompile(`https://hooks\.slack\.com/services/T[A-Za-z0-9]+/B[A-Za-z0-9]+/[A-Za-z0-9]+`),
	regexp.MustCompile(`https://discord(?:app)?\.com/api/webhooks/\d+/[A-Za-z0-9_-]+`),
	regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}`),
}

// dsnPassword is a connection URL's password: group 1 keeps scheme and
// user, group 2 is the password. A placeholder (<pw>, ${PGPASSWORD}) is
// not one.
var dsnPassword = regexp.MustCompile(`(?i)\b((?:postgres(?:ql)?|mysql|mongodb(?:\+srv)?|mssql|redis|rediss|amqp|amqps)://[^:\s/@]*:)([^@\s]+)@`)

// privateKey is a PEM private key block; privateKeyOpen is one whose END
// never arrived in this string (a clipped cat), blanked to the string end.
var (
	privateKey     = regexp.MustCompile(`(?s)-----BEGIN (?:[A-Z ]+ )?PRIVATE KEY-----.*?-----END (?:[A-Z ]+ )?PRIVATE KEY-----`)
	privateKeyOpen = regexp.MustCompile(`(?s)-----BEGIN (?:[A-Z ]+ )?PRIVATE KEY-----.*`)
)

const redacted = "[redacted]"

// secretAssign is a name-keyed credential: password=…, AWS_SECRET_ACCESS_KEY=…,
// api_key: hex. The value must look generated (credentialShaped);
// "token: jsonwebtoken" and "password= in .env" are prose.
var secretAssign = regexp.MustCompile(`(?i)\b[A-Za-z0-9_]*(?:password|passwd|pwd|secret|api[_-]?key|access[_-]?key|auth[_-]?token|token)\s*[=:]\s*["']?([A-Za-z0-9+/=_\-\.]{8,})`)

// credentialShaped: a digit or base64 symbol, or long enough that no
// English word fits.
func credentialShaped(v string) bool {
	v = strings.TrimRight(v, ".")
	if len(v) < 8 {
		return false
	}
	for _, r := range v {
		if (r >= '0' && r <= '9') || r == '+' || r == '/' || r == '=' {
			return true
		}
	}
	return len(v) >= 24
}

var (
	fromSecretKey = regexp.MustCompile(`(?i)\bfrom_secret\s*:\s*["']?$`)
	dottedRef     = regexp.MustCompile(`^[A-Za-z_$][A-Za-z_$]*(?:\.[A-Za-z_$][A-Za-z_$]*)+$`)
	lowerPath     = regexp.MustCompile(`^(?:/|\./|~/|refs/)[a-z0-9_./~-]*$`)
	slashWords    = regexp.MustCompile(`^[a-z]+(?:/[a-z]+)+$`)
	envPlacehold  = regexp.MustCompile(`^(?:\$\{[^}]+\}|\$[A-Z_][A-Z0-9_]*|<[^>]+>|\{\{?[^}]+\}\}?)$`)
)

// assignedSecret: the value of a name-keyed assignment (match is the
// whole secretAssign match, v its value) is a credential, not a
// reference to one. References: a CI secret name (from_secret:
// android_keystore_b64), a digit-free dotted code path
// (cookieStore.refreshToken) or a known env/module namespace
// (process.env.X, base64.b64decode), a lowercase path or refspec
// (/etc/prometheus/…, refs/for/main), a slash-joined package (next/font).
// Each exemption is that exact shape: a weak password in snake_case, a
// hyphenated passphrase, or base64 that opens with / still redacts.
func assignedSecret(match, v string) bool {
	if !credentialShaped(v) {
		return false
	}
	name := match[:strings.LastIndex(match, v)]
	if fromSecretKey.MatchString(name) {
		return false
	}
	low := strings.ToLower(v)
	for _, ns := range []string{"process.env.", "os.environ", "import.meta.env.", "base64."} {
		if strings.HasPrefix(low, ns) {
			return false
		}
	}
	return !dottedRef.MatchString(v) && !lowerPath.MatchString(v) && !slashWords.MatchString(v)
}

// placeholder is a stand-in a doc or command writes where the password
// goes: <pw>, ${PGPASSWORD}, $PGPASSWORD, {{password}}, ****, or our own
// [redacted].
func placeholder(v string) bool {
	return v == redacted || envPlacehold.MatchString(v) || strings.Trim(v, "*xX.") == ""
}

var sensitivePath = regexp.MustCompile(`(?:^|/)(?:\.env(?:\..+)?|\.envrc|.*\.pem|id_rsa|id_rsa\.pub|id_ed25519|id_ed25519\.pub|id_ecdsa|id_dsa|authorized_keys|credentials(?:\.json)?|aws-exports\.js)$`)

func ContainsSecret(text string) bool {
	for _, re := range secrets {
		if re.MatchString(text) {
			return true
		}
	}
	for _, m := range dsnPassword.FindAllStringSubmatch(text, -1) {
		if !placeholder(m[2]) {
			return true
		}
	}
	for _, m := range secretAssign.FindAllStringSubmatch(text, -1) {
		if assignedSecret(m[0], m[1]) {
			return true
		}
	}
	return false
}

// Scrub blanks each secret's own span: a token whole, a URL's password,
// an assignment's value, a private key block. The rest of the text stays.
// A blank runs to the end of the token (tokenEnd): a pattern's character
// class can stop inside a secret, and the tail must not survive.
func Scrub(text string) string {
	text = privateKey.ReplaceAllString(text, redacted)
	text = privateKeyOpen.ReplaceAllString(text, redacted)
	for _, re := range secrets {
		text = blankGroup(text, re, 0, nil)
	}
	text = blankGroup(text, dsnPassword, 2, func(_, v string) bool { return placeholder(v) })
	text = blankGroup(text, secretAssign, 1, func(m, v string) bool { return !assignedSecret(m, v) })
	return text
}

// blankGroup blanks submatch g of each match, through the end of its
// token, unless keep (given the whole match and the value) says the value
// is not a secret.
func blankGroup(text string, re *regexp.Regexp, g int, keep func(match, v string) bool) string {
	idx := re.FindAllStringSubmatchIndex(text, -1)
	if idx == nil {
		return text
	}
	var b strings.Builder
	last := 0
	for _, m := range idx {
		lo, hi := m[2*g], m[2*g+1]
		if lo < last || lo < 0 || (keep != nil && keep(text[m[0]:m[1]], text[lo:hi])) {
			continue
		}
		hi = tokenEnd(text, hi)
		b.WriteString(text[last:lo])
		b.WriteString(redacted)
		last = hi
	}
	b.WriteString(text[last:])
	return b.String()
}

// tokenEnd extends i to the end of the token it sits in: the next space,
// quote, or closing punctuation. A URL password already ends at its @.
func tokenEnd(text string, i int) int {
	for i < len(text) && !strings.ContainsRune(" \t\r\n\"'`,;)]}<>@", rune(text[i])) {
		i++
	}
	return i
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
		if p == "" || traversalPath(p) || remotePath(p) || gitDirPath(p) || depDirPath(p) || scratchPath(p) || sensitivePath.MatchString(p) {
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

func scratchPath(p string) bool {
	n := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(p), "\\", "/"))
	if n == "tmp" || strings.HasPrefix(n, "tmp/") {
		return true
	}
	return n == "qa-report.md"
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

// Line returns the line to append to raw. A JSON line with a secret keeps
// every byte but the secret spans inside its string values; a line that is
// not JSON, or still holds a secret after that, becomes {"_redacted":true}.
func Line(line string) string {
	trim := strings.TrimSpace(line)
	if trim == "" {
		return line
	}
	if ContainsSecret(line) {
		if out, ok := scrubJSONStrings(strings.TrimRight(line, "\r\n")); ok {
			return out + "\n"
		}
		b, _ := json.Marshal(map[string]bool{"_redacted": true})
		return string(b) + "\n"
	}
	if !strings.HasSuffix(line, "\n") {
		return line + "\n"
	}
	return line
}

// scrubJSONStrings runs Scrub over each JSON string token of line (keys
// and values, decoded so escapes are real text) and splices the changed
// ones back re-encoded. Bytes outside changed strings are untouched.
func scrubJSONStrings(line string) (string, bool) {
	if !json.Valid([]byte(line)) {
		return "", false
	}
	var b strings.Builder
	last := 0
	for i := 0; i < len(line); i++ {
		if line[i] != '"' {
			continue
		}
		j := i + 1
		for j < len(line) && line[j] != '"' {
			if line[j] == '\\' {
				j++
			}
			j++
		}
		if j >= len(line) {
			return "", false
		}
		tok := line[i : j+1]
		var text string
		if json.Unmarshal([]byte(tok), &text) == nil {
			// A key whose END is not in this string continues in strings
			// the key regex never sees (one string per file line in an
			// Edit patch): drop the line.
			if privateKeyOpen.MatchString(privateKey.ReplaceAllString(text, "")) {
				return "", false
			}
			if clean := Scrub(text); clean != text {
				b.WriteString(line[last:i])
				b.WriteString(jsonString(clean))
				last = j + 1
			}
		}
		i = j
	}
	b.WriteString(line[last:])
	out := b.String()
	if !json.Valid([]byte(out)) || ContainsSecret(out) {
		return "", false
	}
	return out, true
}

func jsonString(s string) string {
	var buf strings.Builder
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(s)
	return strings.TrimSuffix(buf.String(), "\n")
}
