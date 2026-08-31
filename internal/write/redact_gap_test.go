package write

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"lossless/internal/claim"
)

// storeHoldsPlaintext walks the whole lossless home for a secret substring.
// Redaction is a promise about bytes at rest, wherever they land.
func storeHoldsPlaintext(t *testing.T, root, needle string) bool {
	t.Helper()
	found := false
	_ = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		b, err := os.ReadFile(p)
		if err == nil && strings.Contains(string(b), needle) {
			found = true
			t.Logf("plaintext secret in %s", p)
		}
		return nil
	})
	return found
}

// Every ingest path redacts; remember must not be the one door that does not.
func TestRememberRejectsSecretText(t *testing.T) {
	st := tmpStore(t)
	secret := "ghp_abcdefghijklmnopqrstuvwxyz123456"
	_, err := Remember(st, claim.Record{
		Type: "decision", ProjectKey: "acme/api",
		Text: "the deploy token is " + secret,
	})
	if err == nil {
		t.Fatal("remember with a secret must be rejected")
	}
	ids, err := st.IDsByType("acme/api", "decision", "", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 0 {
		t.Fatalf("secret claim stored: %v", ids)
	}
	if storeHoldsPlaintext(t, st.Root, secret) {
		t.Fatal("secret persisted under the lossless home")
	}
}

// Messages-based catch-up stages a virtual JSONL. It must be redacted on
// write and removed after ingest, not parked in spool/ forever.
func TestVirtualSpoolRedactedAndRemoved(t *testing.T) {
	st := tmpStore(t)
	secret := "ghp_abcdefghijklmnopqrstuvwxyz123456"
	_, err := CatchUp(st, CatchUpRequest{
		Project: "acme/api", Harness: "claude", SessionID: "virt1", Source: "turn",
		Messages: []map[string]any{
			{"type": "message", "role": "user", "content": "the deploy key is " + secret},
			{"type": "message", "role": "assistant", "content": "noted, rotating it now"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(st.Root, "spool", "virtual-virt1.jsonl")); !os.IsNotExist(err) {
		t.Fatalf("virtual spool file left behind (stat err=%v)", err)
	}
	if storeHoldsPlaintext(t, st.Root, secret) {
		t.Fatal("secret persisted under the lossless home")
	}
}

// A failed enqueue must not advance the home-push cursor: home would be
// permanently behind, and every later job kept-but-never-accepted.
func TestHomePushCursorOnlyAdvancesOnEnqueue(t *testing.T) {
	t.Setenv("LOSSLESS_URL", "https://home.example:9")
	st := tmpStore(t)
	req := CatchUpRequest{Project: "acme/api", Harness: "grok", SessionID: "s1"}

	// Block the spool dir with a plain file so WritePush fails.
	if err := os.RemoveAll(filepath.Join(st.Root, "spool")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(st.Root, "spool"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	enqueueHomePush(st, req, "line one\n")
	if got := st.Cursor("home-push:s1"); got != 0 {
		t.Fatalf("cursor advanced to %d on failed enqueue", got)
	}

	if err := os.Remove(filepath.Join(st.Root, "spool")); err != nil {
		t.Fatal(err)
	}
	enqueueHomePush(st, req, "line one\n")
	if got := st.Cursor("home-push:s1"); got != int64(len("line one\n")) {
		t.Fatalf("cursor=%d after successful enqueue", got)
	}
	files, err := ListPush(st.Root)
	if err != nil || len(files) != 1 {
		t.Fatalf("spooled jobs=%v err=%v", files, err)
	}
}
