package gate

import "testing"

// Shapes from the post-0.1.45 active `failed` records (2026-10-25 onward),
// redacted: test-first reports where a new test fails on purpose.
func TestRedStepDropsTestFirstReports(t *testing.T) {
	for _, s := range []string{
		"Every new test failed first and then passed.",
		"**RED:** before the implementation, `node --test tests/js/room-page.test.js` failed 3 tests.",
		"RED was 12 failed before the code, with 404/405 on the routes.",
		"I captured RED on the Python tests (14 failed with src stashed).",
		"JS tests were seen red against the old call.js (7 failed).",
		"For items 1 and 2 the new test failed before the fix (pytest 3 failed, node 2 failed).",
		"The 4 new Python tests and the 2 new JS tests failed with src stashed (RED) and pass now.",
		"Tests: tests/test_pipeline_stream.py went from 12 failed (RED) to 13 passed.",
		"Watched it fail: bulk test FAILED at the waiting-notice assertion, 1 of 45.",
		"Tests: new tests RED 8 failed (old code), GREEN 12 passed in test_voices.py.",
		"I wrote the implementation first, and my attempt to revert `rooms_api.py` to capture RED was denied, so I left it.",
	} {
		if !RedStep(s) {
			t.Errorf("test-first report not a red step: %q", s)
		}
	}
}

func TestRedStepKeepsRealFailures(t *testing.T) {
	for _, s := range []string{
		// no test in sight: a fix that came after a real outage
		"The upload failed for every signed-out user before the fix landed.",
		// red as a CI color, not a test-first step
		"Option 1 is in, but targeted CI #2161 is red: 260/261 passed, 1 failed.",
		"The perf file re-run alone at load 49 failed 2 of 2, so `npm test` still goes red on a loaded machine.",
		// a red step that also traces a root cause
		"The new test failed first, and bisecting it to commit 9e452fc showed the cache key changed.",
		"The new test failed first because the migration regressed in 2.3.",
		// an infra flake on a first run, and old as a plain adjective
		"The PR's first run failed on an infra timeout (a Go test package timed out at 7 minutes) and passed on a re-run.",
		"Moving the towels before the window does not fit on the old grids: the heat test needs a warm stretch.",
		// a weak marker with no new test and no pass after reads the same
		// as a post-mortem, so it stays
		"Tests: the fuzzy and leading-digit tests failed on the old code (4 failures).",
		"Before the fixes, 3 Python tests and 1 JS test failed.",
		// "failed first on X" reads the same as a post-mortem
		"They failed first on an ImportError for REFUSED_HINT, and all 9 pass now.",
		"The checkout test failed first on Safari, then on Chrome, which means the cookie is the problem.",
		"The canary test failed first in us-east and then everywhere; the cert had expired.",
		"The flaky test failed first in CI and then passed on rerun.",
		"Every new test failed first, and the old suite then failed to compile on Go 1.22.",
		"The checkout test failed first with a timeout.",
		"The e2e test was red on CI and green locally.",
		// review probes: post-mortems and CI colors
		"CI red: npm ci failed with ERESOLVE on react 19.",
		"Pipeline #2161 red: the arm64 build failed linking libssl.",
		"I saw red on the arm64 runner: the go test step failed with a segfault.",
		"The node process failed with ENOMEM before the change to the worker pool.",
		"The auth test failed before the fix: the token cache ignored the tenant id.",
		"The e2e suite failed against the previous build with a 502 from nginx.",
		"The integration test failed before the fix landed, and it took prod down for an hour.",
	} {
		if RedStep(s) {
			t.Errorf("real failure read as a red step: %q", s)
		}
	}
}
