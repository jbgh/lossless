package retrieve

import (
	"testing"

	"lossless/internal/claim"
)

// Rows stored before the narration gates (live memora, 2026-09-22) read as
// noise now, so ask stops packing them and inspect --prune retires them.
// Real failures and decisions of the same shapes still pack.
func TestExtractNoiseDropsStoredNarration(t *testing.T) {
	noise := []claim.Record{
		{Type: "failed", Source: "turn", Text: "Empty output — the push may have failed or the pattern missed."},
		{Type: "failed", Source: "turn", Text: "Either the secret update didn't take effect the way I think, or the failure is unrelated."},
		{Type: "failed", Source: "turn", Text: "Revert them if you want the brief kept strictly."},
		{Type: "failed", Source: "turn", Text: "My second edit targeted text that actually lives in `README.md`, so the whole call was rejected.", Paths: []string{"README.md"}},
		{Type: "decision", Source: "turn", Text: "So passing the wrong id (for example the photo's id instead of the album's) would still pass.", Paths: []string{"backend/api/albums_test.go"}},
	}
	for _, r := range noise {
		if !extractNoise(r) {
			t.Errorf("stored narration still packs: %q", r.Text)
		}
	}
	keep := []claim.Record{
		{Type: "failed", Source: "turn", Text: "The build failed on CI in backend/api, and the retry may have failed for a different reason.", Paths: []string{"backend/api"}},
		{Type: "failed", Source: "turn", Text: "Had to revert the limiter change in src/limit.go because it might break the Edge build.", Paths: []string{"src/limit.go"}},
		{Type: "decision", Source: "turn", Text: "We chose jose because jsonwebtoken would break on the Edge runtime in src/middleware/auth.ts.", Paths: []string{"src/middleware/auth.ts"}},
		{Type: "failed", Source: "remember", Text: "Either the cache or the CDN failed; we never found out which."},
		{Type: "decision", Source: "turn", Text: "Use jose, not jsonwebtoken, in src/middleware/auth.ts: jsonwebtoken would break on the Edge runtime.", Paths: []string{"src/middleware/auth.ts"}},
		{Type: "failed", Source: "turn", Text: "The push may have failed, so I reverted the migration in db/migrate/0042.sql.", Paths: []string{"db/migrate/0042.sql"}},
		{Type: "failed", Source: "turn", Text: "The call was rejected with a 401 by the payments API in src/pay/client.go.", Paths: []string{"src/pay/client.go"}},
		{Type: "failed", Source: "turn", Text: "Either way, the deploy failed in deploy/prod.sh for both the arm64 or amd64 images.", Paths: []string{"deploy/prod.sh"}},
	}
	for _, r := range keep {
		if extractNoise(r) {
			t.Errorf("real claim dropped: %q", r.Text)
		}
	}
}
