package retrieve

import "testing"

// v1Ranker pins a test to the v1 pack. v1 stays for one release as a way
// back (LOSSLESS_RANKER=v1), so the behaviours only it has keep their
// tests: the recent-failed net, the per-type cap, the conflict drops and
// the recurrence warning. v2 (rankv2.go) has none of them on purpose.
func v1Ranker(t *testing.T) {
	t.Helper()
	t.Setenv("LOSSLESS_RANKER", "v1")
}
