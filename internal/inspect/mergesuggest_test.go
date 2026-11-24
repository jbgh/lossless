package inspect

import (
	"os"
	"path/filepath"
	"testing"

	"lossless/internal/claim"
	"lossless/internal/store"
)

func TestSuggestMergesFindsOldKeysOfOneCheckout(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	dev := t.TempDir()
	repo := filepath.Join(dev, "game")
	other := filepath.Join(dev, "tool")
	for _, d := range []string{repo, other} {
		if err := os.MkdirAll(filepath.Join(d, ".git"), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	keyOf := map[string]string{repo: "acme/game", other: "acme/tool"}
	resolve := func(ws string) string {
		if k, ok := keyOf[ws]; ok {
			return k
		}
		return "path-" + filepath.Base(ws)
	}

	n := 0
	put := func(project, ws, text string) {
		t.Helper()
		n++
		r := claim.Record{
			ID: claim.NewID(), Type: "decision", Text: text, ProjectKey: project,
			WorkspaceRoot: ws, Harness: "claude", SessionID: "s",
			CreatedAt: "2026-10-01T00:00:00Z", Status: "active", Source: "remember",
		}
		if _, err := st.WriteClaim(r); err != nil {
			t.Fatal(err)
		}
	}
	// The repo's old remote name, written from the checkout and from a
	// worktree that is gone now.
	put("acme/old-game", repo, "Shops close at dusk.")
	put("acme/old-game", filepath.Join(repo, ".claude", "worktrees", "agent-1"), "Keep the day loop at eight minutes.")
	// A path key from before the remote.
	put("path-game", repo, "Use the pixel font for the HUD.")
	// The current key is not suggested into itself.
	put("acme/game", repo, "The HUD badge stays visible.")
	// A note filed under the tool from inside the game checkout: one of
	// many tool records, so the tool is not folded into the game.
	put("acme/tool", repo, "Cross-repo note.")
	put("acme/tool", other, "Tool rule one.")
	put("acme/tool", other, "Tool rule two.")
	put("acme/tool", other, "Tool rule three.")
	put("acme/tool", other, "Tool rule four.")
	// One note written from the game checkout must not speak for a key
	// whose other records name no folder at all.
	put("acme/notes", repo, "Cross-repo note two.")
	put("acme/notes", "", "Notes rule one.")
	put("acme/notes", "", "Notes rule two.")
	// A gone folder whose parent is not a checkout resolves to nothing.
	put("path-gone", filepath.Join(dev, "deleted-sibling"), "Orphan note.")

	got, err := SuggestMerges(st, resolve)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"acme/old-game": "acme/game", "path-game": "acme/game"}
	if len(got) != len(want) {
		t.Fatalf("got %+v, want %v", got, want)
	}
	for _, s := range got {
		if want[s.From] != s.Into {
			t.Fatalf("unexpected suggestion %+v", s)
		}
		if s.Located != s.Total || s.Total == 0 {
			t.Fatalf("located and total should be reported: %+v", s)
		}
	}
}
