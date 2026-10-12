package env

import (
	"os"
	"path/filepath"
	"testing"
)

func TestHome(t *testing.T) {
	t.Setenv("LOSSLESS_HOME", "/tmp/ll")
	if Home() != "/tmp/ll" {
		t.Fatal(Home())
	}
	t.Setenv("LOSSLESS_HOME", "")
	root := t.TempDir()
	t.Setenv("HOME", root)
	if Home() != filepath.Join(root, ".lossless") {
		t.Fatal(Home())
	}
}

func TestHomeTagIgnoresSpelling(t *testing.T) {
	real := t.TempDir()
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	// A home reached through a symlink is the same store as the real path:
	// restore's daemon guard turns on these agreeing.
	if HomeTag(link) != HomeTag(real) {
		t.Fatalf("symlink %s and %s must share a tag", link, real)
	}
	if HomeTag(filepath.Join(real, ".")) != HomeTag(real) {
		t.Fatal("a trailing dot must not change the tag")
	}
	// A path that is not there yet still gets a stable tag, and it differs
	// from every other home.
	missing := filepath.Join(real, "not", "there")
	if HomeTag(missing) != HomeTag(missing) || HomeTag(missing) == HomeTag(real) {
		t.Fatal("a missing path must hash stably and distinctly")
	}
}

func TestURLTokenSidecarClient(t *testing.T) {
	t.Setenv("LOSSLESS_URL", "http://ll")
	if URL() != "http://ll" {
		t.Fatal(URL())
	}
	t.Setenv("LOSSLESS_SIDECAR", "")
	if Sidecar() != "http://127.0.0.1:7432" {
		t.Fatal(Sidecar())
	}
	t.Setenv("LOSSLESS_SIDECAR", "off")
	if Sidecar() != "" {
		t.Fatal(Sidecar())
	}
	t.Setenv("LOSSLESS_SIDECAR", "http://side")
	if Sidecar() != "http://side" {
		t.Fatal(Sidecar())
	}
	t.Setenv("LOSSLESS_CLIENT", "c1")
	if Client() != "c1" {
		t.Fatal(Client())
	}
	t.Setenv("LOSSLESS_TOKEN", "tok")
	if Token() != "tok" {
		t.Fatal(Token())
	}
}

func TestCanonicalURL(t *testing.T) {
	if CanonicalURL(" https://home.example/mcp/ ") != "https://home.example" {
		t.Fatal(CanonicalURL(" https://home.example/mcp/ "))
	}
	if CanonicalURL("http://127.0.0.1:7432") != "http://127.0.0.1:7432" {
		t.Fatal(CanonicalURL("http://127.0.0.1:7432"))
	}
	if CanonicalURL("") != "" {
		t.Fatal("empty")
	}
	t.Setenv("LOSSLESS_URL", "https://home.example/mcp")
	if BaseURL() != "https://home.example" {
		t.Fatal(BaseURL())
	}
}

func TestNewToken(t *testing.T) {
	a, err := NewToken()
	if err != nil || len(a) != 48 {
		t.Fatalf("%q %v", a, err)
	}
	b, _ := NewToken()
	if a == b {
		t.Fatal("not random")
	}
}
