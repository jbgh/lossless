package gate

import "testing"

// 2026-10-05 store check: automated review sessions put a unified diff in
// the user prompt, and its lines were stored as constraints ("never",
// "must" inside a code comment). These are the shapes that were stored.
func TestCodeLineIsNotProse(t *testing.T) {
	for _, s := range []string{
		"+ * The treatment every row in the active list carries and the archived list never does: a highlight",
		"+    // the toast fades at the card's lower edge, never inside it: a faint \"+$100\" ghost showed through the chart",
		"+  /** The card must be opaque (not a translucent wash over the image behind it).",
		"+const ACCENT = 0xc2e24c; // yellow-green rim (never null)",
		"-const ACCENT = 0xb2d870; // yellow-green rim (never null)",
		"-    // the old rule: never draw the card over the banner",
		"+  // instead of hiding it.",
		"// A touch-only phone (no hover, coarse pointer) never sees a keyboard — show the swipe hints",
		"/** The atlas scale for the quality tier: full size on High, half on Low (so it can never exceed the width).",
		"expect(progressMeter(state({ active: 'boost' }))).toBeNull(); // duration 0: never a timed meter",
		"max(1, total + missed.length), `never reached: ${missed.join(', ')}`).toBeLessThanOrEqual(0.05);",
		"const lift = 3 * arc; // tossed up and outward, the arc never crosses the banner",
		`"""The account has no such profile: deleted there, or never made.`,
		"@@ -138,6 +138,24 @@ function highlight(list: Row[], x: number) {",
		"+    assert_rows(view, total), // never more than the page holds",
		"+            except Exception as error:  # one stuck call must not leave the others running",
		"+    The server must not be started while a call is in progress.",
		"+    assert len(failed) >= 2, \"a row and a page both failed to load\"",
		"-        Worked out on every request, never kept in the cached list",
		"-    return cache.get(key, never_expires);",
		"+# The clone needs ten seconds; twenty must be the default.",
		`get("Cache-Control", "").startswith("no-store"):  # a route may add to no-store, never loosen it`,
		`"confidence": 0.6,`,
		"+ENV BUILD_MODE=copy DOWNLOADS=never \\",
		"+is never kept (no-cache), so a page from a new deploy would otherwise run beside a stylesheet",
		"+- The formal form must carry over, and the English must carry it too",
		"+self.count = never  # reset on the next tick",
		"+obj.close(); // must run",
		"*/",
		`"blocked": true`,
		`"fix": "Do not log the token-bearing name.`,
		`"vulnerableCode": "except OSError as error:  # one file that will not go must not keep the others\n    logger.`,
	} {
		if !CodeLine(s) && !CodeStatement(s) {
			t.Fatalf("code line kept as prose: %q", s)
		}
	}
}

func TestCodeLineSparesProse(t *testing.T) {
	for _, s := range []string{
		"Never commit secrets to src/config.ts.",
		"- Never push to main without CI.",
		"* We must always run go vet before a release.",
		"+1, we must always run the tests first.",
		"-race must never be skipped in CI.",
		"--no-verify must never be used on this repo.",
		"-1 is never a valid offset here.",
		"Don't use // comments in the JSON fixtures.",
		"We use https://example.com/docs, never the mirror.",
		"store.Close() must always run before the test returns.",
		"Always call `cleanup();` after the bench.",
		"The limiter in src/middleware/auth.ts failed under load; we reverted it.",
		"Picked jose over jsonwebtoken (smaller, maintained).",
		"I chose x/time/rate instead of Redis for the limiter.",
		`We must always set "strict": true in tsconfig.json.`,
		`"Never again" is the rule for force-pushes to main.`,
		`"Ship": always a single commit on main, then the tag.`,
		"+ We always get type checks with the strict flag.",
		"-   Never push to main without CI.",
		"cleanup() must always run before the store closes,",
		// Review round: names and bullets that open like code.
		"+page.server.ts must never import from src/lib/client/store.ts.",
		"//internal/gate:gate must never depend on //internal/write:write.",
		`"strict": true must always stay set in tsconfig.json.`,
		"-   Never push to main without CI (see .github/workflows/ci.yml)",
		"-  Always run go vet on internal/gate/gate.go (and the eval),",
		"+  faster builds in ci/main.yml, we must always keep that",
		"Never write `foo(); // bar` in src/theme.ts.",
		`"Ship": "one commit" is always the rule on main.`,
		`"Done": "tests pass in internal/write and CI is green", never anything less.`,
		"store.Close() must always run before we return (even on error),",
		"TODO(jay): never ship internal/write without CI;",
		"- # of retries in src/retry.go must never exceed 3",
		"+layout.svelte, +page.svelte: never fetch in both.",
		// Third review round.
		"+page.server.ts's load must never import src/lib/client/store.ts.",
		"+page.svelte/+page.ts must never fetch in both.",
		"*/5 * * * * must never be the cron in deploy/cron.yml.",
		"store.Close() must always run before we return (even on error);",
		"parse(x) panics on nil input in internal/y.go when called from handler(req);",
		"The build failed in src/a.go when the cache was cold.\n\n# Repro: run make twice",
		"The line `const ACCENT = 0xc2e24c; // rim` must never change in src/theme.ts.",
	} {
		if CodeLine(s) || CodeStatement(s) {
			t.Fatalf("prose dropped as a code line: %q", s)
		}
	}
}

func TestHunkHeaderCounts(t *testing.T) {
	if o, n, ok := HunkHeader("@@ -138,6 +138,24 @@ func x() {"); !ok || o != 6 || n != 24 {
		t.Fatalf("got %d %d %v", o, n, ok)
	}
	if o, n, ok := HunkHeader("@@ -5 +5 @@"); !ok || o != 1 || n != 1 {
		t.Fatalf("omitted counts: got %d %d %v", o, n, ok)
	}
	if _, _, ok := HunkHeader("see @@ -1,2 +1,2 @@ above"); ok {
		t.Fatal("a cited header mid-line is not a hunk")
	}
}

// A bare call statement is its own, weaker shape: read time does not apply
// it to stored failures (a workflow finding can read like one).
func TestCodeStatementIsSeparateFromCodeLine(t *testing.T) {
	for _, s := range []string{
		"expect(missed, 'never reached').toEqual([]);",
		"max(1, total + missed.length), `never reached: ${missed.join(', ')}`).toBeLessThanOrEqual(0.05);",
		`error("The mixer failed to load from the CDN");`,
		"updateRows(list, never) {",
	} {
		if !CodeStatement(s) || CodeLine(s) {
			t.Fatalf("%q: statement=%v line=%v", s, CodeStatement(s), CodeLine(s))
		}
	}
	if CodeStatement("parse(x) panics on nil input when the handler passes none.") {
		t.Fatal("a sentence that opens with a call is prose")
	}
}
