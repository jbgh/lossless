package redact

import (
	"strings"
	"testing"
)

func TestContainsSecret(t *testing.T) {
	yes := []string{
		"key AKIAIOSFODNN7EXAMPLE leaked",
		"-----BEGIN RSA PRIVATE KEY-----",
		"Authorization: Bearer " + strings.Repeat("a", 24),
		"token ghp_" + strings.Repeat("b", 20),
		"openai sk-" + strings.Repeat("c", 20),
		"github_pat_" + strings.Repeat("d", 22),
		"sk-ant-" + strings.Repeat("e", 22),
		"sk-proj-" + strings.Repeat("p", 24),
		"xai-" + strings.Repeat("x", 24),
		"AIza" + strings.Repeat("G", 35),
		"sk_live_" + strings.Repeat("f", 24),
		"xoxb-" + strings.Repeat("g", 12),
		"npm_" + strings.Repeat("h", 20),
		"eyJhbGciOiJIUzI1NiJ9." + strings.Repeat("a", 12) + "." + strings.Repeat("b", 12),
		"postgres://user:hunter2@localhost/db",
	}
	for _, s := range yes {
		if !ContainsSecret(s) {
			t.Fatalf("missed secret in %q", s)
		}
	}
	if ContainsSecret("Use jose, not jsonwebtoken, for Edge.") {
		t.Fatal("false positive")
	}
}

func TestShouldDropClaim(t *testing.T) {
	if !ShouldDropClaim("AKIAIOSFODNN7EXAMPLE", nil) {
		t.Fatal("secret text")
	}
	if !ShouldDropClaim("API_KEY=foo", []string{".env"}) {
		t.Fatal(".env assignment")
	}
	if !ShouldDropClaim("this SECRET is in id_rsa", []string{"id_rsa"}) {
		t.Fatal("secret word + sensitive path")
	}
	if ShouldDropClaim("ordinary claim", []string{"src/auth.ts"}) {
		t.Fatal("false drop")
	}
	if ShouldDropClaim("no equals here", []string{".env"}) {
		t.Fatal("sensitive path without leak signal")
	}
}

func TestFilterPaths(t *testing.T) {
	got := FilterPaths([]string{"src/auth.ts", ".env", "keys/id_rsa", "cert.pem", "src/ok.go"})
	if len(got) != 2 || got[0] != "src/auth.ts" || got[1] != "src/ok.go" {
		t.Fatalf("%v", got)
	}
	got = FilterPaths([]string{"src/../../.ssh/id_rsa", "/etc/passwd", `C:\Windows\win.ini`, "foo/../../../etc/shadow", "pkg/foo.go", "~/.ssh/id_rsa"})
	if len(got) != 1 || got[0] != "pkg/foo.go" {
		t.Fatalf("traversal: %v", got)
	}
}

func TestLineRedacts(t *testing.T) {
	got := Line(`{"content":"AKIAIOSFODNN7EXAMPLE"}`)
	if ContainsSecret(got) {
		t.Fatalf("redacted line still secret: %s", got)
	}
	if !strings.Contains(got, `_redacted`) {
		t.Fatalf("marker: %s", got)
	}
	if Line("") != "" {
		t.Fatal("empty")
	}
	if Line("   \n") != "   \n" {
		t.Fatal("whitespace")
	}
	plain := Line("hello")
	if plain != "hello\n" {
		t.Fatalf("newline: %q", plain)
	}
	if Line("hello\n") != "hello\n" {
		t.Fatal("already nl")
	}
}
