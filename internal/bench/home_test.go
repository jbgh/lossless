package bench

import (
	"os"
	"testing"

	"lossless/internal/write"
)

// Bench ingest works only because the home name satisfies the
// isolated-test-store pattern in write.GoTestPath. Pin the cross-package
// contract for both temp-home shapes so a rename or regex change cannot
// silently zero the scorecard again.
func TestBenchHomeAdmitsFixtureIngest(t *testing.T) {
	for _, pattern := range []string{
		"TestBench" + alnumOnly("auth-cross-model") + "*000", // RunDir
		"TestBench*000", // cmd/lossless runBench
	} {
		d, err := os.MkdirTemp("", pattern)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.RemoveAll(d) })
		if !write.GoTestPath(d) {
			t.Fatalf("bench home %q does not read as a test store", d)
		}
	}
}
