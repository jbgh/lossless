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
	regexp.MustCompile(`\bsk-[A-Za-z0-9]{20,}`),
}

var sensitivePath = regexp.MustCompile(`(?:^|/)(?:\.env(?:\..+)?|.*\.pem|id_rsa|credentials)$`)

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
		if !sensitivePath.MatchString(p) {
			out = append(out, p)
		}
	}
	return out
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
