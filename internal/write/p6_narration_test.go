package write

import (
	"strings"
	"testing"
)

// Narration that stored as failed/decision on the live memora tape on
// 2026-09-22: a guess about what failed, an offer to the user, a
// hypothetical, and the agent's own edit tool refusing a call. Real
// failures and decisions in the same messages still extract.
func TestExtractSkipsSpeculationOffersHypotheticals(t *testing.T) {
	drop := []string{
		"Empty output — the push may have failed or the pattern missed.",
		"Either the secret update didn't take effect the way I think, or the failure is unrelated.",
		"Revert them if you want the brief kept strictly.",
		"The assertion only checks the status code in backend/api/albums_test.go. So passing the wrong id (for example the photo's id instead of the album's) would still pass.",
		"My second edit targeted text that actually lives in `README.md`, so the whole call was rejected.",
	}
	keep := []string{
		"Pipeline 5666's smoke-test failed with a 401 on the rotated smoke account.",
		"Woodpecker's secret PUT returns 200 and writes nothing, so the rotation failed silently.",
		"The cutover failed because `apply.sh --preserve` read an empty IMAGE_TAG from the fresh temp file.",
		"We decided to use sqlc instead of GORM for the query layer in backend/db.",
		"Had to revert the limiter change in src/limit.go because it might break the Edge build.",
		"The build failed on CI in backend/api, and the retry may have failed for a different reason.",
		"We chose jose because jsonwebtoken would break on the Edge runtime in src/middleware/auth.ts.",
		"If you want to reproduce it: the deploy failed on memora-eu with a 502 from Caddy.",
		// review 2026-09-22: stored at HEAD, must still store
		"Use jose, not jsonwebtoken, in src/middleware/auth.ts: jsonwebtoken would break on the Edge runtime.",
		"Switched src/db/query.go to sqlc instead of GORM since GORM would fail on the generated types.",
		"The push may have failed, so I reverted the migration in db/migrate/0042.sql.",
		"The call was rejected with a 401 by the payments API in src/pay/client.go.",
		"Either way, the deploy failed in deploy/prod.sh for both the arm64 or amd64 images.",
	}
	for _, s := range drop {
		got := Extract([]Message{{Role: "assistant", Text: s, Offset: 1}}, ExtractOpts{ProjectKey: "memora/memora"})
		if len(got) != 0 {
			t.Errorf("narration stored as %s: %q", got[0].Type, s)
		}
	}
	for _, s := range keep {
		got := Extract([]Message{{Role: "assistant", Text: s, Offset: 1}}, ExtractOpts{ProjectKey: "memora/memora"})
		if len(got) != 1 || !strings.Contains(got[0].Text, s[:20]) {
			t.Errorf("real claim lost: %q -> %+v", s, got)
		}
	}
}
