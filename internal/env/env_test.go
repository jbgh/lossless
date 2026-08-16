package env

import (
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

func TestURLTokenSidecarClient(t *testing.T) {
	t.Setenv("LOSSLESS_URL", "http://ll")
	if URL() != "http://ll" {
		t.Fatal(URL())
	}
	t.Setenv("LOSSLESS_SIDECAR", "")
	if Sidecar() != "http://127.0.0.1:7432" {
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
