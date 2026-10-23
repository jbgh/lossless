package projectkey

import (
	"testing"
	"time"
)

// fakeResolver swaps the git-backed resolver and the clock for a test.
func fakeResolver(t *testing.T, answer *string) (calls *int, clock *time.Time) {
	t.Helper()
	n := 0
	at := time.Unix(1_800_000_000, 0)
	prevResolve, prevNow := cachedResolve, cachedNow
	cachedResolve = func(string) string { n++; return *answer }
	cachedNow = func() time.Time { return at }
	resetCache()
	t.Cleanup(func() {
		cachedResolve, cachedNow = prevResolve, prevNow
		resetCache()
	})
	return &n, &at
}

// A hook catch-up and an ask each resolved the workspace through git
// (one or two processes) on every call.
func TestCachedReusesAnOriginKey(t *testing.T) {
	key := "acme/api"
	calls, clock := fakeResolver(t, &key)
	for i := 0; i < 5; i++ {
		if got := Cached("/ws/api"); got != "acme/api" {
			t.Fatalf("got %q", got)
		}
	}
	if *calls != 1 {
		t.Fatalf("resolved %d times, want 1", *calls)
	}
	*clock = clock.Add(cachedOriginTTL + time.Second)
	key = "acme/api-renamed"
	if got := Cached("/ws/api"); got != "acme/api-renamed" || *calls != 2 {
		t.Fatalf("after TTL got %q with %d resolves", got, *calls)
	}
}

// A path-hash key is what a workspace gets before it has an origin. Once
// the user adds one, records must move to owner/repo soon, so this kind
// of answer is kept only briefly.
func TestCachedExpiresAPathKeyQuickly(t *testing.T) {
	key := "path-0123456789abcdef"
	calls, clock := fakeResolver(t, &key)
	Cached("/ws/new")
	*clock = clock.Add(cachedPathTTL / 2)
	Cached("/ws/new")
	if *calls != 1 {
		t.Fatalf("resolved %d times inside the path TTL, want 1", *calls)
	}
	key = "acme/new"
	*clock = clock.Add(cachedPathTTL)
	if got := Cached("/ws/new"); got != "acme/new" || *calls != 2 {
		t.Fatalf("after path TTL got %q with %d resolves", got, *calls)
	}
}

// Workspaces are cached separately: two worktrees of one repo, or two
// repos, never share an answer.
func TestCachedKeysByWorkspace(t *testing.T) {
	key := "a/b"
	calls, _ := fakeResolver(t, &key)
	Cached("/ws/one")
	Cached("/ws/two")
	Cached("/ws/one/")
	if *calls != 2 {
		t.Fatalf("resolved %d times, want 2 (one per workspace)", *calls)
	}
	if Cached("") != "" || *calls != 2 {
		t.Fatal("empty workspace must resolve to nothing without git")
	}
}

// Expired entries are dropped once the cache grows, so a daemon that sees
// many short-lived worktrees does not keep them all.
func TestCachedPrunesExpiredEntries(t *testing.T) {
	key := "path-x"
	_, clock := fakeResolver(t, &key)
	for i := 0; i < cachedMaxEntries; i++ {
		Cached("/ws/" + time.Duration(i).String())
	}
	*clock = clock.Add(cachedPathTTL + time.Second)
	Cached("/ws/fresh")
	cache.Lock()
	n := len(cache.m)
	cache.Unlock()
	if n != 1 {
		t.Fatalf("cache holds %d entries after expiry, want 1", n)
	}
}
