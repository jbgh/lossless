package write

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// reviewPrompt is the shape an automated review session opens with: a
// machine-written user turn that carries the whole diff.
const reviewPrompt = `Review this change for security vulnerabilities.

Changed files (you may Read these and any other file in the repo):
  - src/ui/list/rows.ts

Unified diff (only + lines are new):

=== DIFF: src/ui/list/rows.ts ===
@@ -138,6 +138,12 @@ function highlight(list: Row[], x: number) {
   list.push(GLOW, x, 0.04);
 }

+const ACTIVE = [1, 0.5, 0.06] as const; // the warm amber highlight
+/**
+ * The treatment every active row carries and the archived row never does.
+ * Badges must stay lit while the row is active. Always amber.
+ */
+function markActive(list: Row[]) {}
 /**
  * Sorted rows. The header must always lead the group.
  * Never sorted here before.
@@ -200,3 +206,3 @@ export function updateRows() {
   row.moveTo(0, 1);
-const ACCENT = 0xb2d870; // rim (never null)
+const ACCENT = 0xc2e24c; // rim (never null)
   row.rotate(0);
`

func TestStripNonProseDropsCountedHunks(t *testing.T) {
	got := stripNonProse(reviewPrompt + "\nNever touch src/ui/legacy.ts.\n")
	for _, gone := range []string{"never does", "must stay lit", "Always amber", "must always lead", "Never sorted", "never null", "@@"} {
		if strings.Contains(got, gone) {
			t.Fatalf("diff text %q survived:\n%s", gone, got)
		}
	}
	for _, kept := range []string{"Review this change", "src/ui/list/rows.ts", "Never touch src/ui/legacy.ts."} {
		if !strings.Contains(got, kept) {
			t.Fatalf("prose %q was dropped:\n%s", kept, got)
		}
	}
}

// A hunk ends where its counts run out or at the first line that cannot be
// a diff line: prose after a short hunk is not eaten.
func TestStripNonProseStopsAtProse(t *testing.T) {
	in := "@@ -1,2 +1,3 @@\n a\n+b never\n c\n- We must never skip CI on main.\nPlain prose never stops here.\n" +
		"@@ -5,9 +5,9 @@ truncated hunk\n x\n+y always\nBack to prose: always run the linter.\n"
	got := stripNonProse(in)
	want := "- We must never skip CI on main.\nPlain prose never stops here.\nBack to prose: always run the linter.\n"
	if got != want {
		t.Fatalf("got %q\nwant %q", got, want)
	}
}

func TestStripNonProseDropsCodeFences(t *testing.T) {
	in := "Here is the patch:\n```diff\n-old line never used\n+new line must stay\n```\nWe must keep the old name.\n```go\nx := 1 // never zero\n```\n"
	got := stripNonProse(in)
	if strings.Contains(got, "never used") || strings.Contains(got, "must stay") {
		t.Fatalf("diff fence survived: %q", got)
	}
	if !strings.Contains(got, "We must keep the old name.") || strings.Contains(got, "x := 1") {
		t.Fatalf("want the prose kept and the go fence dropped: %q", got)
	}
	if plain := "No diff here.\nNever was."; stripNonProse(plain) != plain {
		t.Fatalf("plain text changed: %q", stripNonProse(plain))
	}
}

func TestExtractIgnoresDiffInUserPrompt(t *testing.T) {
	if recs := extractAs("user", reviewPrompt); len(recs) != 0 {
		t.Fatalf("diff lines stored as claims: %s", typesOf(recs))
	}
	recs := extractAs("user", reviewPrompt+"\nNever touch src/ui/legacy.ts.\n")
	if len(recs) != 1 || recs[0].Type != "constraint" || recs[0].Text != "Never touch src/ui/legacy.ts." {
		t.Fatalf("want the one typed constraint, got: %s", typesOf(recs))
	}
}

// Code that reaches a user turn outside a diff (a pasted snippet, a
// truncated hunk) is still not the user's rule.
func TestExtractIgnoresCodeLines(t *testing.T) {
	for _, s := range []string{
		"+    // the toast fades at the card's lower edge, never inside it",
		"// a touch-only phone never sees a keyboard",
		"expect(meter(state({ active: 'boost' }))).toBeNull(); // duration 0: never a timed meter",
	} {
		if recs := extractAs("user", s); len(recs) != 0 {
			t.Fatalf("code line stored: %s", typesOf(recs))
		}
	}
	if recs := extractAs("user", "- Never push to main without CI in .github/workflows/ci.yml."); len(recs) != 1 {
		t.Fatalf("bulleted rule dropped: %s", typesOf(recs))
	}
}

// A follow-up review prompt carries the earlier findings as JSON. Their
// string values were cut into sentences and stored as constraints
// ("Previously the name was never logged.").
func TestExtractIgnoresJSONMembersInUserPrompt(t *testing.T) {
	in := "You previously flagged these candidate vulnerabilities:\n\n[\n  {\n" +
		"    \"filePath\": \"src/files_api.py\",\n" +
		"    \"explanation\": \"The file name is logged. Previously the name was never logged. A cleanup that is skipped must not hold a lock.\",\n" +
		"    \"fix\": \"Do not log the token-bearing name.\",\n" +
		"    \"confidence\": 0.6\n  }\n]\n"
	if recs := extractAs("user", in); len(recs) != 0 {
		t.Fatalf("JSON values stored as claims: %s", typesOf(recs))
	}
	recs := extractAs("user", in+"\nNever log the token in src/files_api.py.\n")
	if len(recs) != 1 || recs[0].Text != "Never log the token in src/files_api.py." {
		t.Fatalf("want the one typed constraint, got: %s", typesOf(recs))
	}
	// A quoted term with a prose gloss is the user's sentence.
	if recs := extractAs("user", `"Ship": always a single commit in .github/workflows/release.yml, never two.`); len(recs) != 1 {
		t.Fatalf("glossed term dropped: %s", typesOf(recs))
	}
	// A sentence that quotes a key is not a JSON member line.
	if recs := extractAs("user", `We must always set "strict": true in tsconfig.json.`); len(recs) != 1 {
		t.Fatalf("prose naming a JSON key dropped: %s", typesOf(recs))
	}
}

// claudeUserLine is one Claude Code transcript line, so a test can take
// the parse path (harness chrome, the long-turn clip) before extract.
func claudeUserLine(t *testing.T, text string) string {
	t.Helper()
	b, err := json.Marshal(map[string]any{
		"type": "user", "message": map[string]any{"role": "user", "content": text},
	})
	if err != nil {
		t.Fatal(err)
	}
	return string(b) + "\n"
}

func extractParsed(t *testing.T, text string) string {
	t.Helper()
	msgs, _ := ParseJSONL(claudeUserLine(t, text), 0)
	return typesOf(Extract(msgs, ExtractOpts{ProjectKey: "acme/api"}))
}

// A long turn is clipped to its head and tail at parse. Clipped first, the
// tail of a big diff had no hunk header left and its context lines were
// stored ("The header must always lead the group.").
func TestExtractStripsDiffBeforeTheClip(t *testing.T) {
	var b strings.Builder
	b.WriteString("Review this change for security vulnerabilities.\n\n@@ -10,203 +10,203 @@ export function updateRows() {\n")
	for i := 0; i < 100; i++ {
		b.WriteString("   row.moveTo(0, 1); // the cursor must never leave the list\n")
		b.WriteString("-  The header must always lead the group in src/ui/list/rows.ts.\n")
		b.WriteString("+  The footer must always close the group in src/ui/list/rows.ts.\n")
	}
	for i := 0; i < 3; i++ {
		b.WriteString("  * Never sort src/ui/list/rows.ts here before the header.\n")
	}
	if b.Len() < 2*clipWhole {
		t.Fatalf("fixture too short to clip: %d", b.Len())
	}
	if got := extractParsed(t, b.String()); got != "" {
		t.Fatalf("diff text stored from a clipped turn: %s", got)
	}
	want := "constraint:Never touch src/ui/legacy.ts."
	if got := extractParsed(t, b.String()+"\nNever touch src/ui/legacy.ts.\n"); got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

// The excerpt view of a turn is unchanged: only extract reads the strip.
func TestParseKeepsTextForExcerpts(t *testing.T) {
	in := "Here is the patch:\n@@ -1,1 +1,1 @@\n-old line never used\n+new line must stay\nWe must keep the old name in src/a.go.\n"
	msgs, _ := ParseJSONL(claudeUserLine(t, in), 0)
	if len(msgs) != 1 || !strings.Contains(msgs[0].Text, "+new line must stay") {
		t.Fatalf("Text lost the pasted block: %+v", msgs)
	}
	if p := msgs[0].prose(); strings.Contains(p, "must stay") || !strings.Contains(p, "We must keep the old name") {
		t.Fatalf("prose = %q", p)
	}
	plain, _ := ParseJSONL(claudeUserLine(t, "Never touch src/ui/legacy.ts."), 0)
	if len(plain) != 1 || plain[0].prose() != plain[0].Text {
		t.Fatalf("a turn with nothing pasted reads the same both ways: %+v", plain)
	}
}

func TestStripNonProseLeavesAnUnclosedFence(t *testing.T) {
	in := "The patch I tried:\n```diff\n-old\n+new\nNever touch src/ui/legacy.ts.\nWe must always keep src/ui/rows.ts sorted.\n"
	got := extractAs("user", in)
	if len(got) != 2 {
		t.Fatalf("an unclosed fence swallowed the turn: %s", typesOf(got))
	}
}

// A header someone cites, or a hunk cut short, is followed by a blank line
// and then their own bullets.
func TestStripNonProseStopsAtBulletsAfterABlankLine(t *testing.T) {
	in := "In the hunk\n@@ -138,6 +138,24 @@ function highlight(list: Row[]) {\n\n- Never draw the card over the banner in src/ui/card.ts.\n- Always keep src/ui/rows.ts sorted.\n"
	if got := extractAs("user", in); len(got) != 2 {
		t.Fatalf("bullets after a cited header were eaten: %s", typesOf(got))
	}
	// A real blank context line inside a hunk still belongs to it.
	hunk := "@@ -1,4 +1,4 @@\n a must never\n\n-b always\n+c always\n d must never\nNever touch src/ui/legacy.ts.\n"
	if got := stripNonProse(hunk); got != "Never touch src/ui/legacy.ts.\n" {
		t.Fatalf("got %q", got)
	}
}

func TestStripNonProseDropsLanguageFencesAndWholeLineJSON(t *testing.T) {
	in := "Look at this:\n```go title=\"a.go\"\nreturn nil // always succeed for src/a.go\n```\n" +
		"```python\nvalue = load()  # never call this before src/init.py runs\n```\n" +
		"{\"explanation\": \"Previously the name in src/files_api.py was never logged.\", \"confidence\": 0.6}\n" +
		"[\n  {\n    \"note\": [\n      \"We must never log src/files_api.py names.\"\n    ]\n  }\n]\n"
	if got := extractAs("user", in); len(got) != 0 {
		t.Fatalf("fenced code or JSON stored: %s", typesOf(got))
	}
	// A bare fence may hold notes, and JSON inside a sentence is part of it.
	keep := "```\nNever touch src/ui/legacy.ts.\n```\nThe health route in src/health.go must always return {\"ok\": true} as JSON.\n"
	got := extractAs("user", keep)
	if len(got) != 2 || !strings.Contains(typesOf(got), `{"ok": true}`) {
		t.Fatalf("want both sentences whole, got: %s", typesOf(got))
	}
}

// Workflow findings are failure memory whatever their phrasing, a quoted
// statement included.
func TestWorkflowFindingQuotingCodeStillStores(t *testing.T) {
	in := "{\"asked\":true,\"findings\":[{\"issue\":\"`defer f.Close(); // never reached` in internal/x.go leaks the fd when Open fails\",\"severity\":\"high\"}," +
		"{\"issue\":\"parse(x) panics on nil input in internal/y.go when called from handler(req);\",\"severity\":\"high\"}]}"
	got := extractParsed(t, in)
	if !strings.Contains(got, "internal/x.go leaks the fd") || !strings.Contains(got, "parse(x) panics") {
		t.Fatalf("workflow findings dropped: %q", got)
	}
}

// Codex text used to be clipped before finishMessage saw it, so the strip
// ran on a diff that had already lost its header.
func TestCodexStripsDiffBeforeTheClip(t *testing.T) {
	var b strings.Builder
	b.WriteString("Review this change.\n\n@@ -10,202 +10,202 @@ export function updateRows() {\n")
	for i := 0; i < 100; i++ {
		b.WriteString("-  The header must always lead the group in src/ui/list/rows.ts.\n")
		b.WriteString("+  The footer must always close the group in src/ui/list/rows.ts.\n")
	}
	b.WriteString("  * Never sort src/ui/list/rows.ts here before the header.\n")
	b.WriteString("  * Never sort src/ui/list/cols.ts here before the header.\n")
	b.WriteString("\nNever touch src/ui/legacy.ts.\n")
	line, err := json.Marshal(map[string]any{
		"type": "event_msg", "payload": map[string]any{"type": "user_message", "message": b.String()},
	})
	if err != nil {
		t.Fatal(err)
	}
	msgs, _ := ParseJSONL(string(line)+"\n", 0)
	if len(msgs) != 1 || msgs[0].Role != "user" {
		t.Fatalf("fixture did not parse as a Codex user turn: %+v", msgs)
	}
	if got, want := typesOf(Extract(msgs, ExtractOpts{ProjectKey: "acme/api"})), "constraint:Never touch src/ui/legacy.ts."; got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

// A long turn with two fenced blocks: clipped first, the opener left in
// the head paired with the second block's opener in the tail, swallowed
// the rule between them, and let the second block's comment through.
func TestExtractStripsFencesBeforeTheClip(t *testing.T) {
	var b strings.Builder
	b.WriteString("Two files.\n```go\n")
	for i := 0; i < 150; i++ {
		b.WriteString("x = append(x, row) // the cursor must never leave the list.\n")
	}
	b.WriteString("```\nNever touch src/ui/legacy.ts.\n```ts\n * We must never sort src/ui/b.ts here.\n```\nAlways keep src/ui/a.ts sorted.\n")
	if b.Len() < 2*clipWhole {
		t.Fatalf("fixture too short to clip: %d", b.Len())
	}
	got := extractParsed(t, b.String())
	if got != "constraint:Never touch src/ui/legacy.ts. | constraint:Always keep src/ui/a.ts sorted." {
		t.Fatalf("got %q", got)
	}
}

// A long JSON dump: clipped first, the tail began inside a string value
// and its sentences read as the user's.
func TestExtractStripsJSONBeforeTheClip(t *testing.T) {
	var items []map[string]string
	for i := 0; i < 12; i++ {
		items = append(items, map[string]string{
			"explanation": "The cache in src/cache.py must never be shared across tenants. We must always rotate keys in src/keys.py. " + strings.Repeat("More detail follows here. ", 30),
		})
	}
	pretty, _ := json.MarshalIndent(items, "", "  ")
	compact, _ := json.Marshal(items)
	for name, in := range map[string]string{"pretty": string(pretty), "compact": string(compact)} {
		if len(in) < 2*clipWhole {
			t.Fatalf("%s fixture too short to clip: %d", name, len(in))
		}
		if got := extractParsed(t, "You previously flagged:\n"+in+"\n"); got != "" {
			t.Fatalf("%s JSON stored: %s", name, got)
		}
	}
	strs := "[\n  \"We must never log src/files_api.py names.\",\n  \"Always close src/db.py handles.\"\n]\n"
	if got := extractAs("user", strs); len(got) != 0 {
		t.Fatalf("array of strings stored: %s", typesOf(got))
	}
}

// Only the first sentence of a comment line carries the marker; the line
// goes as a whole.
func TestExtractDropsEverySentenceOfACodeLine(t *testing.T) {
	for _, in := range []string{
		"// Never mutate the cache in src/cache.go. Always copy src/cache.go rows first.",
		"+    // The server must not be started twice. Never call start in src/server.go again.",
	} {
		if got := extractAs("user", in); len(got) != 0 {
			t.Fatalf("%q stored: %s", in, typesOf(got))
		}
	}
}

// A log or a shell session in a tagged fence is where a failure gets
// reported; it reads like a bare fence.
func TestExtractKeepsLogFences(t *testing.T) {
	in := "I ran the suite.\n```console\nTestClip failed in internal/write/parse_test.go: clip cut a rune.\n```\n"
	bare := strings.Replace(in, "```console", "```", 1)
	if a, b := typesOf(extractAs("assistant", in)), typesOf(extractAs("assistant", bare)); a != b || a == "" {
		t.Fatalf("console fence %q, bare fence %q", a, b)
	}
}

// A line is consumed only while its own side of the hunk has lines left,
// so a truncated new-file hunk does not take the bullets after it as
// removed lines.
func TestStripNonProseHunkSidesAndCRLF(t *testing.T) {
	in := "@@ -0,0 +1,200 @@\n+line one\n+line two\n- Never push to main without CI in .github/workflows/ci.yml.\n- Always run go vet on internal/gate/gate.go.\n"
	if got := extractAs("user", in); len(got) != 2 {
		t.Fatalf("bullets after a truncated hunk were eaten: %s", typesOf(got))
	}
	crlf := "@@ -1,4 +1,4 @@\r\n a must never\r\n\r\n-b always\r\n+c always\r\n d must never\r\nNever touch src/ui/legacy.ts.\r\n"
	if got := stripNonProse(crlf); strings.Contains(got, "always") || !strings.Contains(got, "Never touch src/ui/legacy.ts.") {
		t.Fatalf("CRLF hunk: %q", got)
	}
}

// A diff of a document carries that document's fences as context lines.
// The hunk is stepped over by its counts, so one of them does not close
// the block the diff was pasted in.
func TestFenceAroundADiffThatContainsAFence(t *testing.T) {
	in := "```diff\n@@ -10,8 +10,8 @@ intro\n Some intro.\n ```\n x := 1\n ```\n The header must always lead the group in src/ui/list/rows.ts.\n-We must never sort src/ui/list/rows.ts here.\n+We must always sort src/ui/list/rows.ts here.\n Never touch src/ui/legacy.ts in this path.\n more\n last\n```\nAlways keep src/ui/a.ts sorted.\n"
	if got, want := typesOf(extractAs("user", in)), "constraint:Always keep src/ui/a.ts sorted."; got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

// A header someone cites is followed by their own list, whatever the
// bullets open with, blank line or not.
func TestCitedHunkHeaderKeepsTheListUnderIt(t *testing.T) {
	list := "- **Never** draw the card over the banner in src/ui/card.ts.\n- [ ] Always keep src/ui/rows.ts sorted.\n- 3 retries: never more in src/retry.go.\n"
	for _, gap := range []string{"\n", ""} {
		in := "In the hunk\n@@ -138,6 +138,24 @@ function highlight(list: Row[]) {\n" + gap + list
		if got := stripNonProse(in); !strings.Contains(got, "3 retries: never more") || !strings.Contains(got, "Always keep src/ui/rows.ts") {
			t.Fatalf("gap %q: list eaten: %q", gap, got)
		}
	}
	// A removed comment line under a blank context line is still the hunk.
	hunk := "@@ -1,3 +1,2 @@\n keep\n\n- * The old rule: never sort here.\nNever touch src/ui/legacy.ts.\n"
	if got := stripNonProse(hunk); got != "Never touch src/ui/legacy.ts.\n" {
		t.Fatalf("got %q", got)
	}
}

func TestSentenceThatOpensWithAFenceTagIsProse(t *testing.T) {
	in := "```go fences are dropped; bare ones stay.\nNever touch src/ui/legacy.ts.\n```\nnotes\n```\nAlways keep src/ui/rows.ts sorted.\n"
	if got := extractAs("user", in); len(got) != 2 {
		t.Fatalf("got %s", typesOf(got))
	}
}

// The JSON scan is one pass. A turn of unclosed braces (log lines cut to
// width) used to rescan to the end of the turn from every line: 200 KB
// took 13 s.
func TestStripNonProseIsLinearOnUnclosedJSON(t *testing.T) {
	for _, line := range []string{
		`{"ts":"2026-10-06T00:00:00Z","level":"info","msg":"request served`,
		`{ "k": 1,`,
		`[{"a": [1, 2, {"b": "c"`,
	} {
		text := strings.Repeat(line+"\n", 2<<20/len(line))
		done := make(chan string, 1)
		go func() { done <- clipProse(text) }()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatalf("clipProse did not finish on %d bytes of %q", len(text), line)
		}
	}
	// Valid values after an abandoned one are still found.
	in := "{ \"k\": 1,\n\n{\"explanation\": \"We must never log src/files_api.py names.\"}\nNever touch src/ui/legacy.ts.\n"
	if got := typesOf(extractAs("user", in)); got != "constraint:Never touch src/ui/legacy.ts." {
		t.Fatalf("got %q", got)
	}
}

// Tool output is never extracted, so parse keeps no second copy of it.
func TestParseKeepsNoFullTextForToolRole(t *testing.T) {
	line, _ := json.Marshal(map[string]any{"role": "tool", "content": strings.Repeat("{ \"k\": 1,\n", 1000)})
	msgs, _ := ParseJSONL(string(line)+"\n", 0)
	if len(msgs) != 1 || msgs[0].Role != "tool" || msgs[0].full != "" {
		t.Fatalf("got %+v", msgs)
	}
}
