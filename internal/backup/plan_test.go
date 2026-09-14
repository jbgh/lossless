// internal/backup/plan_test.go
package backup

import (
	"strings"
	"testing"
)

func item(rel, sha string) Item {
	return Item{Rel: rel, SHA256: sha, Size: 1, Object: "o/n-" + rel + "/" + sha}
}

func TestMakePlanDiffsAgainstPointer(t *testing.T) {
	pointer := emptyManifest()
	pointer.Generations = []string{"g2", "g1"}
	pointer.Files["a"] = FileEntry{SHA256: "1", Object: "o/n-a/1"}
	pointer.Files["b"] = FileEntry{SHA256: "1", Object: "o/n-b/1"}
	pointer.Files["gone"] = FileEntry{SHA256: "1", Object: "o/n-gone/1"}
	p := makePlan([]Item{item("a", "1"), item("b", "2"), item("new", "1")}, pointer, 5, "g3")
	if p.NoChange || len(p.Upload) != 2 || p.Upload[0].Rel != "b" || p.Upload[1].Rel != "new" {
		t.Fatalf("%+v", p)
	}
	if strings.Join(p.Removed, ",") != "gone" {
		t.Fatalf("removed %v", p.Removed)
	}
	if strings.Join(p.Kept, ",") != "g3,g2,g1" || len(p.Drop) != 0 {
		t.Fatalf("kept %v drop %v", p.Kept, p.Drop)
	}
	if p.Files["b"].SHA256 != "2" || len(p.Files) != 3 {
		t.Fatalf("files %+v", p.Files)
	}
}

func TestMakePlanNoChangeAndKeep(t *testing.T) {
	pointer := emptyManifest()
	pointer.Generations = []string{"g3", "g2", "g1"}
	pointer.Dropping = []string{"g0"}
	pointer.Files["a"] = FileEntry{SHA256: "1", Object: "o/n-a/1"}
	p := makePlan([]Item{item("a", "1")}, pointer, 2, "g4")
	if !p.NoChange || strings.Join(p.Kept, ",") != "g3,g2,g1" || len(p.Drop) != 0 {
		t.Fatalf("no change must keep the pointer's generations: %+v", p)
	}
	p = makePlan([]Item{item("a", "2")}, pointer, 2, "g4")
	if p.NoChange || strings.Join(p.Kept, ",") != "g4,g3" || strings.Join(p.Drop, ",") != "g2,g1" {
		t.Fatalf("keep=2: kept %v drop %v", p.Kept, p.Drop)
	}
	// A generation already under Dropping is never re-kept.
	pointer.Generations = []string{"g3", "g0"}
	p = makePlan([]Item{item("a", "2")}, pointer, 5, "g4")
	if strings.Join(p.Kept, ",") != "g4,g3" {
		t.Fatalf("dropping excluded: %v", p.Kept)
	}
}
