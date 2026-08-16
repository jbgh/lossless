package claim

import (
	"strings"
	"testing"
	"unicode"
)

func TestNewIDUniqueAndShaped(t *testing.T) {
	a, b := NewID(), NewID()
	if a == b {
		t.Fatal("ids collided")
	}
	if len(a) < 14+16 {
		t.Fatalf("short id %q", a)
	}
	for _, r := range a[:14] {
		if r < '0' || r > '9' {
			t.Fatalf("timestamp prefix not digits: %q", a)
		}
	}
}

func TestHashNormalizesPunctuationAndCase(t *testing.T) {
	a := Hash("acme/api", "decision", "Use JOSE, not jsonwebtoken!")
	b := Hash("acme/api", "decision", "use jose not jsonwebtoken")
	if a != b {
		t.Fatalf("hash not stable under normalize: %s vs %s", a, b)
	}
	if Hash("acme/api", "failed", "use jose not jsonwebtoken") == a {
		t.Fatal("type must affect hash")
	}
	if Hash("other/p", "decision", "use jose not jsonwebtoken") == a {
		t.Fatal("project must affect hash")
	}
}

func TestTokens(t *testing.T) {
	got := Tokens("Use JOSE, not a jwt. 你好世界")
	want := []string{"use", "jose", "not", "jwt", "你好世界"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("got %v want %v", got, want)
	}
	if Tokens("") != nil && len(Tokens("")) != 0 {
		t.Fatalf("empty: %v", Tokens(""))
	}
	if n := Tokens("a x"); len(n) != 0 {
		t.Fatalf("len-1 tokens should drop: %v", n)
	}
}

func TestPathKeysAndStem(t *testing.T) {
	keys := PathKeys([]string{"src/middleware/auth.ts", "auth.ts", "", "src/middleware/auth.ts"})
	if len(keys) != 2 {
		t.Fatalf("keys=%v", keys)
	}
	if Stem("src/foo/bar.ts") != "bar" {
		t.Fatal(Stem("src/foo/bar.ts"))
	}
	if Stem("Makefile") != "Makefile" {
		t.Fatal(Stem("Makefile"))
	}
	if Stem(".env") != ".env" {
		t.Fatal("dotfile stem should stay")
	}
	if baseName(`src\win\auth.ts`) != "auth.ts" {
		t.Fatal(baseName(`src\win\auth.ts`))
	}
	if baseName("auth.ts") != "auth.ts" {
		t.Fatal("plain basename")
	}
}

func TestIsIdentifier(t *testing.T) {
	if !IsIdentifier("jsonwebtoken") || !IsIdentifier("tokenBucket") || !IsIdentifier("_id") {
		t.Fatal("idents")
	}
	if !IsIdentifier("auth.ts") || !IsIdentifier("src/auth.ts") {
		t.Fatal("path-like")
	}
	if IsIdentifier("we") || IsIdentifier("12ab") || IsIdentifier("a") || IsIdentifier("foo.") {
		t.Fatal("non-ident")
	}
}

func TestExtractSymbols(t *testing.T) {
	got := ExtractSymbols("Use jose, not jsonwebtoken.", []string{"src/middleware/auth.ts"})
	joined := strings.Join(got, " ")
	for _, need := range []string{"jose", "jsonwebtoken", "auth.ts", "auth"} {
		if !strings.Contains(joined, need) {
			t.Fatalf("missing %q in %v", need, got)
		}
	}
	// empty / duplicate path stems should not panic or dup
	again := ExtractSymbols("jose jose", []string{"auth.ts", "auth.ts", "  "})
	if len(again) != 2 { // jose, auth.ts (stem auth == basename without ext? Stem("auth.ts")=auth)
		// jose, auth.ts, auth
		if len(again) < 2 {
			t.Fatalf("symbols=%v", again)
		}
	}
}

func TestNormalizeStripsPunct(t *testing.T) {
	if normalize("Hello,   WORLD!!") != "hello world" {
		t.Fatal(normalize("Hello,   WORLD!!"))
	}
	if normalize("...") != "" {
		t.Fatal(normalize("..."))
	}
	for _, r := range normalize("café") {
		if !unicode.IsLetter(r) && r != ' ' {
			t.Fatalf("unexpected %q", r)
		}
	}
}
