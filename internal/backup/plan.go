// internal/backup/plan.go
package backup

import "sort"

type plan struct {
	Files    map[string]FileEntry // desired set
	Upload   []Item               // (rel, sha) the pointer does not hold
	Removed  []string             // rels the pointer holds that are gone
	Kept     []string             // generations kept after this run, newest first
	Drop     []string             // generations this run pushes out (not the pointer's pending Dropping)
	Existing []string             // the pointer's generations minus its pending Dropping; every one is referenced until the pointer is rewritten
	NoChange bool
}

func filesFrom(items []Item) map[string]FileEntry {
	out := make(map[string]FileEntry, len(items))
	for _, it := range items {
		out[it.Rel] = FileEntry{SHA256: it.SHA256, Size: it.Size, Object: it.Object}
	}
	return out
}

func makePlan(items []Item, pointer *Manifest, keep int, gen string) plan {
	p := plan{Files: filesFrom(items)}
	for _, it := range items {
		if prev, ok := pointer.Files[it.Rel]; !ok || prev.SHA256 != it.SHA256 {
			p.Upload = append(p.Upload, it)
		}
	}
	sort.Slice(p.Upload, func(i, j int) bool { return p.Upload[i].Rel < p.Upload[j].Rel })
	for rel := range pointer.Files {
		if _, ok := p.Files[rel]; !ok {
			p.Removed = append(p.Removed, rel)
		}
	}
	sort.Strings(p.Removed)
	p.NoChange = len(p.Upload) == 0 && len(p.Removed) == 0

	dropping := map[string]bool{}
	for _, g := range pointer.Dropping {
		dropping[g] = true
	}
	seen := map[string]bool{}
	var existing []string
	for _, g := range pointer.Generations {
		if g != "" && !dropping[g] && !seen[g] {
			seen[g] = true
			existing = append(existing, g)
		}
	}
	p.Existing = existing
	if p.NoChange {
		p.Kept = existing
		return p
	}
	if keep < 1 {
		keep = 1
	}
	all := append([]string{gen}, existing...)
	if len(all) > keep {
		p.Kept, p.Drop = all[:keep], all[keep:]
	} else {
		p.Kept = all
	}
	return p
}
