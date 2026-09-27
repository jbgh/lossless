package store

import "testing"

func TestFailedNameSeen(t *testing.T) {
	st := tmp(t)
	if _, err := st.WriteClaim(rec("a1", "failed", "The failure is export.spec.ts (bundled font check).", nil)); err != nil {
		t.Fatal(err)
	}
	if _, err := st.WriteClaim(rec("a2", "failed", "vitest 26 passed, 1 failed in sim-seeded.test.ts", nil)); err != nil {
		t.Fatal(err)
	}
	// _ and % are LIKE wildcards: with LIKE, looking up
	// "sim_seeded.test.ts" also matched this row and reported a
	// never-failed test as already seen.
	if _, err := st.WriteClaim(rec("a3", "failed", "simXseeded.test.ts failed on arm64", nil)); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name   string
		lookup string
		excl   string
		want   bool
	}{
		{"mention", "export.spec.ts", "", true},
		{"case-insensitive", "EXPORT.SPEC.TS", "", true},
		{"never-failed", "auth.spec.ts", "", false},
		{"excluded self", "export.spec.ts", "a1", false}, // a2 does not mention it
		{"excluded only mention", "sim-seeded.test.ts", "a2", false},
		{"excluded other record", "sim-seeded.test.ts", "a1", true},
		{"underscore is not a wildcard", "sim_seeded.test.ts", "", false},
		{"percent is not a wildcard", "auth%spec.ts", "", false},
		{"inside a longer name", "seeded.test.ts", "", false},
	}
	for _, c := range cases {
		got, err := st.FailedNameSeen("acme/api", c.lookup, c.excl, "")
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if got != c.want {
			t.Errorf("%s: got %v want %v", c.name, got, c.want)
		}
	}
}

func TestFailedNameSeenIgnoresProject(t *testing.T) {
	st := tmp(t)
	other := rec("b1", "failed", "export.spec.ts failed on the bundled font check.", nil)
	other.ProjectKey = "other/repo"
	if _, err := st.WriteClaim(other); err != nil {
		t.Fatal(err)
	}
	got, err := st.FailedNameSeen("acme/api", "export.spec.ts", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if got {
		t.Fatal("another project's failure counted as seen")
	}
}

// A superseded record still says the test has failed before; the sweep
// supersedes records in a loop and must not resurrect them.
func TestFailedNameSeenCountsSuperseded(t *testing.T) {
	st := tmp(t)
	if _, err := st.WriteClaim(rec("c1", "failed", "export.spec.ts failed on the bundled font check.", nil)); err != nil {
		t.Fatal(err)
	}
	if err := st.Supersede("c1"); err != nil {
		t.Fatal(err)
	}
	got, err := st.FailedNameSeen("acme/api", "export.spec.ts", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if !got {
		t.Fatal("superseded failure no longer counts as seen")
	}
}

// Only failed records count: a decision that mentions the name is not a
// report of the test failing.
func TestFailedNameSeenIgnoresNonFailed(t *testing.T) {
	st := tmp(t)
	if _, err := st.WriteClaim(rec("d1", "state", "export.spec.ts is the share screen test file.", nil)); err != nil {
		t.Fatal(err)
	}
	got, err := st.FailedNameSeen("acme/api", "export.spec.ts", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if got {
		t.Fatal("a non-failed record counted as a failure report")
	}
}

// Of several reports of one failure only the earliest has nothing before
// it. Without the time bound every report saw the others, and the prune
// sweep retired all of them, the first real report included.
func TestFailedNameSeenCountsOnlyEarlier(t *testing.T) {
	st := tmp(t)
	for _, r := range []struct{ id, at string }{
		{"e1", "2026-09-23T21:42:25Z"},
		{"e2", "2026-09-24T00:59:02Z"},
		{"e3", "2026-09-24T00:59:02Z"},
	} {
		c := rec(r.id, "failed", "vitest 1 failed in tests/render/grid-fit.test.ts", nil)
		c.CreatedAt = r.at
		if _, err := st.WriteClaim(c); err != nil {
			t.Fatal(err)
		}
	}
	cases := []struct {
		self, before string
		want         bool
	}{
		{"e1", "2026-09-23T21:42:25Z", false},
		{"e2", "2026-09-24T00:59:02Z", true},
		{"e3", "2026-09-24T00:59:02Z", true}, // e2 ties on time and sorts first by id
	}
	for _, c := range cases {
		got, err := st.FailedNameSeen("acme/api", "grid-fit.test.ts", c.self, c.before)
		if err != nil {
			t.Fatal(err)
		}
		if got != c.want {
			t.Errorf("%s: got %v want %v", c.self, got, c.want)
		}
	}
}

// SQLite's lower() is ASCII-only; a non-ASCII name must still match its
// capitalized mention.
func TestFailedNameSeenNonASCII(t *testing.T) {
	st := tmp(t)
	if _, err := st.WriteClaim(rec("u1", "failed", "Überweisung.test.ts failed on the rounding case.", nil)); err != nil {
		t.Fatal(err)
	}
	got, err := st.FailedNameSeen("acme/api", "überweisung.test.ts", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if !got {
		t.Fatal("non-ASCII name not matched")
	}
}
