package projectkey

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestFromOrigin(t *testing.T) {
	cases := []string{
		"https://github.com/Acme/API.git",
		"git@github.com:acme/api.git",
		"https://github.com/acme/api",
		"git+https://github.com/Acme/API.git",
	}
	for _, c := range cases {
		if g := FromOrigin(c); g != "acme/api" {
			t.Fatalf("%s -> %q", c, g)
		}
	}
	if FromOrigin("not-a-remote") != "" {
		t.Fatal("expected empty")
	}
	if FromOrigin("") != "" {
		t.Fatal("empty origin")
	}
}

func TestNormalizeAndEncode(t *testing.T) {
	if Normalize("Acme/API") != "acme/api" {
		t.Fatal(Normalize("Acme/API"))
	}
	if Normalize("acme__api") != "acme/api" {
		t.Fatal(Normalize("acme__api"))
	}
	if Encode("acme/api") != "acme__api" {
		t.Fatal(Encode("acme/api"))
	}
	if Normalize("just-a-name") != "just-a-name" {
		t.Fatal(Normalize("just-a-name"))
	}
	if Normalize("  Foo/Bar.git  ") != "foo/bar" {
		t.Fatal(Normalize("  Foo/Bar.git  "))
	}
	if Normalize("/onlyone") != "/onlyone" {
		t.Fatalf("single segment: %q", Normalize("/onlyone"))
	}
	if Normalize("a/b/c") != "b/c" {
		t.Fatalf("last two: %q", Normalize("a/b/c"))
	}
	if Encode("..") != "unknown" || Encode(".") != "unknown" || Encode("") != "unknown" {
		t.Fatalf("traversal: %q %q %q", Encode(".."), Encode("."), Encode(""))
	}
	if Encode("acme/api") != "acme__api" {
		t.Fatal(Encode("acme/api"))
	}
	if Decode("acme__api") != "acme/api" {
		t.Fatal(Decode("acme__api"))
	}
}

func TestFromWorkspaceGitOrigin(t *testing.T) {
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %s", args, out)
		}
	}
	run("init")
	run("remote", "add", "origin", "git@github.com:Acme/Widget.git")
	got := FromWorkspace(dir)
	if got != "acme/widget" {
		t.Fatalf("got %q", got)
	}
}

func TestFromWorkspaceNoGit(t *testing.T) {
	dir := t.TempDir()
	got := FromWorkspace(dir)
	if !strings.HasPrefix(got, "path-") || len(got) != len("path-")+16 {
		t.Fatalf("got %q", got)
	}
	again := FromWorkspace(dir)
	if again != got {
		t.Fatal("unstable path key")
	}
}

func TestFromWorkspaceOriginUnparseable(t *testing.T) {
	dir := t.TempDir()
	cmd := exec.Command("git", "init")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%s", out)
	}
	cmd = exec.Command("git", "remote", "add", "origin", "not-a-url")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%s", out)
	}
	got := FromWorkspace(dir)
	if !strings.HasPrefix(got, "path-") {
		t.Fatalf("expected path fallback, got %q", got)
	}
}

func TestGitOutputFailure(t *testing.T) {
	if gitOutput(filepath.Join(t.TempDir(), "missing"), "status") != "" {
		t.Fatal("expected empty")
	}
}

func TestFromWorkspaceGitOriginEmptyPATH(t *testing.T) {
	dir := t.TempDir()
	cmd := exec.Command("git", "init")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%s", out)
	}
	cmd = exec.Command("git", "remote", "add", "origin", "https://git.memora.pics/memora/memora.git")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%s", out)
	}
	t.Setenv("PATH", "/nonexistent")
	got := FromWorkspace(dir)
	if got != "memora/memora" {
		t.Fatalf("PATH-less git: %q", got)
	}
	ok, detail := Identity(dir)
	if !ok || detail != "memora/memora" {
		t.Fatalf("identity %v %q", ok, detail)
	}
}

func TestIdentityNoGitCwd(t *testing.T) {
	ok, detail := Identity(t.TempDir())
	if !ok {
		t.Fatalf("empty dir should not fail: %q", detail)
	}
}
