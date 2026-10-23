package projectkey

import (
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Cached is FromWorkspace through a per-workspace cache. FromWorkspace
// runs git once or twice (about 15 ms of a child process each, through
// Apple's xcrun shim); the daemon resolved the same workspace that way
// on every hook catch-up and every ask.
//
// An owner/repo key is kept for cachedOriginTTL: an origin changes about
// never. A path-hash key (a workspace with no origin yet) is kept only
// cachedPathTTL, so records move to owner/repo soon after the user adds
// a remote. FromWorkspace stays uncached for doctor and other callers
// that must see the answer as it is right now.
func Cached(root string) string {
	if strings.TrimSpace(root) == "" {
		return ""
	}
	k := filepath.Clean(root)
	now := cachedNow()
	cache.Lock()
	if e, ok := cache.m[k]; ok && now.Sub(e.at) < ttlFor(e.key) {
		cache.Unlock()
		return e.key
	}
	cache.Unlock()
	// Resolved outside the lock: two concurrent misses both run git,
	// which is what every call did before.
	key := cachedResolve(root)
	cache.Lock()
	if len(cache.m) >= cachedMaxEntries {
		for p, e := range cache.m {
			if now.Sub(e.at) >= ttlFor(e.key) {
				delete(cache.m, p)
			}
		}
		// Still full of live entries (hundreds of worktrees within ten
		// minutes): start over rather than grow. A miss costs one git run.
		if len(cache.m) >= cachedMaxEntries {
			cache.m = map[string]cacheEntry{}
		}
	}
	cache.m[k] = cacheEntry{key: key, at: now}
	cache.Unlock()
	return key
}

const (
	cachedOriginTTL  = 10 * time.Minute
	cachedPathTTL    = 30 * time.Second
	cachedMaxEntries = 256
)

func ttlFor(key string) time.Duration {
	if key == "" || strings.HasPrefix(key, "path-") {
		return cachedPathTTL
	}
	return cachedOriginTTL
}

type cacheEntry struct {
	key string
	at  time.Time
}

var cache = struct {
	sync.Mutex
	m map[string]cacheEntry
}{m: map[string]cacheEntry{}}

// Tests replace these to count resolves and move the clock.
var (
	cachedResolve = FromWorkspace
	cachedNow     = time.Now
)

func resetCache() {
	cache.Lock()
	cache.m = map[string]cacheEntry{}
	cache.Unlock()
}
