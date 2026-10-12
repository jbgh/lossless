package write

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"lossless/internal/store"
)

func saidTexts(msgs []Message, so SaidOpts) []string {
	var out []string
	for _, r := range Said(msgs, ExtractOpts{ProjectKey: "acme/api", SessionID: "s1", Harness: "claude", Source: "turn"}, so) {
		if r.Type != "said" || r.TranscriptRef == nil {
			panic("said record without type or cite")
		}
		out = append(out, r.Text)
	}
	return out
}

func userMsgs(texts ...string) []Message {
	var msgs []Message
	for i, t := range texts {
		msgs = append(msgs, Message{Role: "user", Text: t, Offset: int64(i * 1000)})
	}
	return msgs
}

// Extract keeps a user sentence only when a cue word marks it, which
// leaves most lasting instructions with no record at all; none of these
// would have been kept.
func TestSaidKeepsWhatAPersonTyped(t *testing.T) {
	keep := []string{
		"lets use the smaller model for all subagents since its pretty good now",
		"the export should keep the column order people already rely on",
		"Let’s only use the V4 models",
		"can you give me multiple choice with recommendations on things you need my feedback on?",
		"What does the doc say about retries? It should be three attempts, not five.",
	}
	got := saidTexts(userMsgs(keep...), SaidOpts{})
	if len(got) != len(keep) {
		t.Fatalf("kept %d of %d: %q", len(got), len(keep), got)
	}
}

func TestSaidSkipsWhatIsNotMemory(t *testing.T) {
	skip := []string{
		"yes do that",        // too short to mean anything later
		"are we stuck here?", // only a question
		"How much longer until we can ship?",
		"Don't push yet, I'll review after lunch.", // steers this session
		"/compact", // a command
		"Task: Repo: /work/api. You are in a managed worktree. Do NOT edit.", // a brief
		strings.Repeat("please keep going with the long pasted plan ", 60),   // a paste
	}
	if got := saidTexts(userMsgs(skip...), SaidOpts{}); len(got) != 0 {
		t.Fatalf("stored as said: %q", got)
	}
	// Other roles are never the user's words.
	msgs := []Message{
		{Role: "assistant", Text: "I will use the smaller model for all subagents from now on.", Offset: 1},
		{Role: "user", Text: "use the smaller model for all subagents", Offset: 2, Skip: true},
	}
	if got := saidTexts(msgs, SaidOpts{}); len(got) != 0 {
		t.Fatalf("stored a non-user or skipped turn: %q", got)
	}
}

// A child session opens with the brief its parent wrote. One mark of a
// brief is enough for that first turn, and only for that turn.
func TestSaidTreatsTheOpeningTurnAsABrief(t *testing.T) {
	brief := "Implement the image resizer from the project plan. Working directory: /Users/dev/image-tools. " +
		"Create the resize module with the two scaling modes described in the plan, add the unit tests for both, " +
		"and keep the public surface to the three functions the plan names."
	later := "lets also review the plan in /Users/dev/image-tools/plan.md before the next task"
	msgs := userMsgs(brief, later)
	if got := saidTexts(msgs, SaidOpts{TapeStart: true}); len(got) != 1 || got[0] != later {
		t.Fatalf("opening brief: got %q", got)
	}
	if got := saidTexts(msgs, SaidOpts{}); len(got) != 2 {
		t.Fatalf("mid-tape chunk must keep both: %q", got)
	}
	// A person's short opening turn is theirs, path and capitals or not.
	short := userMsgs("do NOT touch /Users/me/app/config.yaml while you fix the retry logic")
	if got := saidTexts(short, SaidOpts{TapeStart: true}); len(got) != 1 {
		t.Fatalf("short human opener dropped: %q", got)
	}
}

func TestSaidKeepsTheWordsAfterAnAttachment(t *testing.T) {
	got := saidTexts(userMsgs(
		"[Image #1] make the header sticky like this, on every page",
		"@src/app/layout.tsx keep this file small, move the providers out",
		"[Image #2]",
		"[Image 1] i see this when i open the editor, lets fix that first",
	), SaidOpts{})
	// The image marker goes. The file mention stays: it is the referent.
	want := []string{"make the header sticky like this, on every page", "@src/app/layout.tsx keep this file small, move the providers out",
		"i see this when i open the editor, lets fix that first"}
	if len(got) != 3 || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Fatalf("got %q", got)
	}
	recs := Said(userMsgs("@src/app/layout.tsx keep this file small, move the providers out"),
		ExtractOpts{ProjectKey: "acme/api", SessionID: "s1"}, SaidOpts{})
	if len(recs) != 1 || len(recs[0].Paths) != 1 || recs[0].Paths[0] != "src/app/layout.tsx" {
		t.Fatalf("a mentioned file must be the record's path: %+v", recs)
	}
}

func TestSaidUnwrapsAMessageDeliveredMidTurn(t *testing.T) {
	wrapped := "The user sent a message while you were working: <user_query> also put the cleanup in the nightly cron job </user_query> Make sure to complete any unfinished tasks first."
	got := saidTexts(userMsgs(wrapped), SaidOpts{})
	if len(got) != 1 || got[0] != "also put the cleanup in the nightly cron job" {
		t.Fatalf("got %q", got)
	}
}

func TestSaidSkipsATemplateOpening(t *testing.T) {
	msgs := userMsgs("Check the batch four workers and continue per the ledger in docs/plan.md")
	seen := func(lead string) bool { return strings.HasPrefix(lead, "Check the batch four workers") }
	if got := saidTexts(msgs, SaidOpts{LeadSeen: seen}); len(got) != 0 {
		t.Fatalf("template stored: %q", got)
	}
}

func TestSaidClipsLongTurnsOnARuneBoundary(t *testing.T) {
	long := strings.Repeat("keep the café menu readable on the small screens é ", 20)
	got := saidTexts(userMsgs(long), SaidOpts{})
	if len(got) != 1 || len(got[0]) > saidClip+4 || !strings.HasSuffix(got[0], "…") {
		t.Fatalf("clip: %d bytes %q", len(got[0]), got)
	}
	if !strings.ContainsRune(got[0], 'é') || strings.ContainsRune(got[0], '�') {
		t.Fatalf("clip cut a rune: %q", got[0])
	}
}

func TestLineTimeReadsTheHarnessTimestamp(t *testing.T) {
	chunk := `{"type":"user","timestamp":"2026-08-03T10:15:00.123Z","message":{"content":"a"}}` + "\n" +
		`{"type":"user","content":"no time here"}` + "\n"
	if got := lineTime(chunk, 0); got.IsZero() || got.UTC().Format(time.RFC3339) != "2026-08-03T10:15:00Z" {
		t.Fatalf("lineTime = %v", got)
	}
	second := int64(strings.Index(chunk, "\n") + 1)
	if got := lineTime(chunk, second); !got.IsZero() {
		t.Fatalf("line without a timestamp dated %v", got)
	}
	if got := lineTime(chunk, 99999); !got.IsZero() {
		t.Fatal("offset past the chunk")
	}
}

// A release that learns to keep something new reads the tapes again:
// the sessions before it are not lost to it.
func TestBackfillAddsUserTurnsFromStoredTapesOnce(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	dir := filepath.Join(st.Root, "raw", "acme__api", "2026-08")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	tape := `{"type":"user","timestamp":"2026-08-03T10:15:00Z","content":"lets use the smaller model for all subagents from now on"}` + "\n" +
		`{"type":"assistant","timestamp":"2026-08-03T10:15:05Z","content":"Understood, switching the subagent model."}` + "\n" +
		`{"type":"user","timestamp":"2026-08-03T10:20:00Z","content":"are we done?"}` + "\n"
	if err := os.WriteFile(filepath.Join(dir, "sess-1.jsonl"), []byte(tape), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(st.Root, "raw", "manual", "2026-08"), 0o700); err != nil {
		t.Fatal(err)
	}
	onRecord(t, st, "sess-1")

	dry, err := Backfill(st, "", true)
	if err != nil || dry.Added != 1 {
		t.Fatalf("dry run: %+v %v", dry, err)
	}
	if recs, _ := st.ListActive("acme/api"); len(recs) != 0 {
		t.Fatalf("dry run wrote %d records", len(recs))
	}
	res, err := Backfill(st, "", false)
	if err != nil || res.Tapes != 1 || res.Added != 1 {
		t.Fatalf("backfill: %+v %v", res, err)
	}
	recs, _ := st.ListActive("acme/api")
	if len(recs) != 1 || recs[0].Type != "said" || recs[0].SessionID != "sess-1" {
		t.Fatalf("records: %+v", recs)
	}
	if recs[0].CreatedAt != "2026-08-03T10:15:00Z" || !strings.HasPrefix(recs[0].ID, "20260803101500") {
		t.Fatalf("turn not dated from the tape: id=%s at=%s", recs[0].ID, recs[0].CreatedAt)
	}
	again, err := Backfill(st, "", false)
	if err != nil || again.Added != 0 {
		t.Fatalf("second pass added %d", again.Added)
	}
	if other, _ := Backfill(st, "other/repo", false); other.Tapes != 0 {
		t.Fatalf("project filter read %d tapes", other.Tapes)
	}
}

// onRecord puts sessions in the store's session table: backfill reads
// only the tapes of sessions still on record.
func onRecord(t *testing.T, st *store.Store, ids ...string) {
	t.Helper()
	for _, id := range ids {
		if err := st.UpsertSession(store.Session{JSONL: "/src/" + id + ".jsonl", SessionID: id, Harness: "grok", Project: "acme/api"}); err != nil {
			t.Fatal(err)
		}
	}
}

func saidOf(t *testing.T, st *store.Store, project string) []string {
	t.Helper()
	recs, err := st.ListActive(project)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, r := range recs {
		if r.Type == "said" {
			out = append(out, r.Text)
		}
	}
	return out
}

// The opening-prompt rule belongs to the first ingest of a session, not
// to whatever chunk happens to start a tape file: a resumed session gets
// a new part, and the user's first turn back is not a brief.
func TestCatchUpKeepsUserTurnsAndKnowsTheSessionOpening(t *testing.T) {
	st := tmpStore(t)
	dir := t.TempDir()
	brief := "Implement the image resizer from the project plan. Working directory: /Users/dev/image-tools. " +
		"Create the resize module with the two scaling modes described in the plan, add the unit tests for both, " +
		"and keep the public surface to the three functions the plan names."
	later := "the build agent should also read /Users/dev/image-tools/notes/scaling-modes.md first, since the plan " +
		"leaves out how the two scaling modes round their results, and that file has the rule we settled on last week " +
		"after the blurry thumbnails showed up"
	line := func(text string) string { return `{"type":"user","content":"` + text + `"}` + "\n" }
	src := writeJSONL(t, dir, "chat.jsonl", line(brief))
	req := CatchUpRequest{JSONL: src, Project: "acme/api", Harness: "grok", SessionID: "child-1"}
	if _, err := CatchUp(st, req); err != nil {
		t.Fatal(err)
	}
	if got := saidOf(t, st, "acme/api"); len(got) != 0 {
		t.Fatalf("the session's opening brief was stored: %q", got)
	}
	if err := os.WriteFile(src, []byte(line(brief)+line(later)), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := CatchUp(st, req); err != nil {
		t.Fatal(err)
	}
	got := saidOf(t, st, "acme/api")
	if len(got) != 1 || !strings.HasPrefix(got[0], "the build agent should also read") {
		t.Fatalf("a later turn with one mark of a brief is the user's: %q", got)
	}
}

// The dry run keeps the same books as the real one: a turn repeated in a
// later part of the session, or in another session, is counted once.
func TestBackfillDryRunCountsWhatTheRealRunAdds(t *testing.T) {
	st := tmpStore(t)
	dir := filepath.Join(st.Root, "raw", "acme__api", "2026-08")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	turn := `{"type":"user","content":"lets use the smaller model for all subagents from now on"}` + "\n"
	other := `{"type":"user","content":"keep the nightly report to one page with the failures first"}` + "\n"
	for name, body := range map[string]string{
		"sess-1.jsonl":       turn,
		"sess-1.part2.jsonl": turn + other,
		"sess-2.jsonl":       turn,
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	onRecord(t, st, "sess-1", "sess-2")
	dry, err := Backfill(st, "", true)
	if err != nil {
		t.Fatal(err)
	}
	res, err := Backfill(st, "", false)
	if err != nil {
		t.Fatal(err)
	}
	if dry.Added != 2 || res.Added != 2 || dry.Turns != res.Turns {
		t.Fatalf("dry %+v, real %+v: want 2 added by both", dry, res)
	}
	if got := saidOf(t, st, "acme/api"); len(got) != 2 {
		t.Fatalf("stored %q", got)
	}
}

// Three sessions that open a turn the same way are a template. The
// session being read does not count against itself.
func TestBackfillSkipsATemplateFromOtherSessionsOnly(t *testing.T) {
	st := tmpStore(t)
	dir := filepath.Join(st.Root, "raw", "acme__api", "2026-08")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	lead := "Check the batch four workers and then continue "
	write := func(name string, tails ...string) {
		var b strings.Builder
		for _, tail := range tails {
			b.WriteString(`{"type":"user","content":"` + lead + tail + `"}` + "\n")
		}
		if err := os.WriteFile(filepath.Join(dir, name), []byte(b.String()), 0o600); err != nil {
			t.Fatal(err)
		}
		// Backfill reads oldest first; keep the order of the calls.
		time.Sleep(15 * time.Millisecond)
	}
	write("s1.jsonl", "with the audio review", "with the hitch benchmark")
	write("s2.jsonl", "with the render review")
	write("s3.jsonl", "with the deploy checklist")
	onRecord(t, st, "s1", "s2", "s3")
	if _, err := Backfill(st, "", false); err != nil {
		t.Fatal(err)
	}
	got := saidOf(t, st, "acme/api")
	// s1's two turns (its own session is not "another"), s2's one. s3 is
	// the third session to use the opening.
	if len(got) != 3 {
		t.Fatalf("stored %d: %q", len(got), got)
	}
	for _, text := range got {
		if strings.Contains(text, "deploy checklist") {
			t.Fatalf("template stored from a third session: %q", got)
		}
	}
	// The live path asks the store the same question: s1 has seen the
	// opening from one other session, a new session from two.
	if templateLead(st, "acme/api", "s1", SaidLead(lead+"x")) {
		t.Fatal("s1 counted itself")
	}
	if !templateLead(st, "acme/api", "s9", SaidLead(lead+"x")) {
		t.Fatal("a fourth session must see the template")
	}
	if SaidLead("Check batch 4 workers and continue") != SaidLead("Check batch 12 workers and continue") {
		t.Fatal("numbers must not split a template")
	}
}

// A said record the user retired stays retired when the tapes are read
// again after an upgrade.
func TestBackfillDoesNotBringBackARetiredRecord(t *testing.T) {
	st := tmpStore(t)
	dir := filepath.Join(st.Root, "raw", "acme__api", "2026-08")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	tape := `{"type":"user","content":"lets use the smaller model for all subagents from now on"}` + "\n"
	if err := os.WriteFile(filepath.Join(dir, "sess-1.jsonl"), []byte(tape), 0o600); err != nil {
		t.Fatal(err)
	}
	onRecord(t, st, "sess-1")
	if res, err := Backfill(st, "", false); err != nil || res.Added != 1 {
		t.Fatalf("backfill: %+v %v", res, err)
	}
	if _, err := st.DB.Exec(`UPDATE records SET status = 'superseded' WHERE type = 'said'`); err != nil {
		t.Fatal(err)
	}
	if res, err := Backfill(st, "", false); err != nil || res.Added != 0 {
		t.Fatalf("a retired turn came back: %+v %v", res, err)
	}
	if got := saidOf(t, st, "acme/api"); len(got) != 0 {
		t.Fatalf("active again: %q", got)
	}
}

// Prune removes a test workspace's session and records and leaves its
// tape. Reading the tapes again must not bring that session back.
func TestBackfillLeavesTapesWithNoSessionOnRecord(t *testing.T) {
	st := tmpStore(t)
	dir := filepath.Join(st.Root, "raw", "acme__api", "2026-08")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	tape := `{"type":"user","content":"lets use the smaller model for all subagents from now on"}` + "\n"
	if err := os.WriteFile(filepath.Join(dir, "pruned-1.jsonl"), []byte(tape), 0o600); err != nil {
		t.Fatal(err)
	}
	res, err := Backfill(st, "", false)
	if err != nil || res.Added != 0 || res.Skipped != 1 {
		t.Fatalf("backfill: %+v %v", res, err)
	}
}

// A session that runs into a new month gets a second tape that counts
// from zero. Its first turn there continues the session; it is not the
// opening prompt, and the opening-brief rule does not apply to it.
func TestBackfillOpeningIsTheSessionsFirstTape(t *testing.T) {
	st := tmpStore(t)
	onRecord(t, st, "long-1")
	later := "the build agent should also read /Users/dev/image-tools/notes/scaling-modes.md first, since the plan " +
		"leaves out how the two scaling modes round their results, and that file has the rule we settled on last week " +
		"after the blurry thumbnails showed up"
	for month, body := range map[string]string{
		"2026-08": `{"type":"user","content":"lets start on the image resizer today, two scaling modes"}` + "\n",
		"2026-09": `{"type":"user","content":"` + later + `"}` + "\n",
	} {
		dir := filepath.Join(st.Root, "raw", "acme__api", month)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "long-1.jsonl"), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// Make the older month's file the newer one on disk, as a late seal does.
	now := time.Now()
	_ = os.Chtimes(filepath.Join(st.Root, "raw", "acme__api", "2026-09", "long-1.jsonl"), now.Add(-time.Hour), now.Add(-time.Hour))
	_ = os.Chtimes(filepath.Join(st.Root, "raw", "acme__api", "2026-08", "long-1.jsonl"), now, now)
	res, err := Backfill(st, "", false)
	if err != nil || res.Added != 2 {
		t.Fatalf("the turn that opens a new month's tape was treated as a brief: %+v %v", res, err)
	}
}

// A brief turned away at the session's opening is still a brief when a
// re-copied tape repeats it mid-file.
func TestSaidRemembersTheOpeningBrief(t *testing.T) {
	brief := "Implement the image resizer from the project plan. Working directory: /Users/dev/image-tools. " +
		"Create the resize module with the two scaling modes described in the plan, add the unit tests for both, " +
		"and keep the public surface to the three functions the plan names."
	briefs := map[string]bool{}
	if got := saidTexts(userMsgs(brief), SaidOpts{TapeStart: true, Briefs: briefs}); len(got) != 0 {
		t.Fatalf("opening brief stored: %q", got)
	}
	if got := saidTexts(userMsgs(brief), SaidOpts{Briefs: briefs}); len(got) != 0 {
		t.Fatalf("the same brief stored from a later part: %q", got)
	}
}

// Claude Code says how a session was started. A person at a terminal can
// open with a page of instructions, and that page is theirs.
func TestCatchUpKeepsALongOpeningPromptFromAPersonAtATerminal(t *testing.T) {
	long := "do NOT touch the migrations while you work on this. the retry logic in the upload client gives up after " +
		"two attempts and that is too early for people on slow connections, so make it patient: more attempts, longer " +
		"waits, and tell the person what is happening while it waits instead of going quiet"
	line := func(entry string) string {
		return `{"type":"user","entrypoint":"` + entry + `","message":{"role":"user","content":"` + long + `"}}` + "\n"
	}
	for entry, want := range map[string]int{"cli": 1, "sdk-py": 0} {
		st := tmpStore(t)
		src := writeJSONL(t, t.TempDir(), "chat.jsonl", line(entry))
		if _, err := CatchUp(st, CatchUpRequest{JSONL: src, Project: "acme/api", Harness: "claude", SessionID: "s-" + entry}); err != nil {
			t.Fatal(err)
		}
		if got := saidOf(t, st, "acme/api"); len(got) != want {
			t.Fatalf("entrypoint %s: stored %d, want %d: %q", entry, len(got), want, got)
		}
	}
}

func TestSaidLeadReadsPastFoldedNumbers(t *testing.T) {
	a := SaidLead("Round 4 reconciliation for batch 12 of the second pass, check the builders")
	b := SaidLead("Round 11 reconciliation for batch 3 of the second pass, check the builders")
	if a != b || len([]rune(a)) != saidLeadRunes {
		t.Fatalf("%q vs %q", a, b)
	}
}

// An IDE puts the open file and the selection in front of what the user
// typed. The block goes; the words are theirs. A tag that is never closed
// is the user writing about markup.
func TestSaidStripsHarnessBlocksInFrontOfTheTurn(t *testing.T) {
	got := saidTexts(userMsgs(
		"<ide_opened_file>The user opened the file /w/app/src/a.ts in the IDE.</ide_opened_file> use the smaller model for all subagents from now on please",
		`<ide_selection lang="ts">const x = 1</ide_selection><ide_opened_file>a.ts</ide_opened_file> keep the helpers in this file private`,
		"<task-notification>the background build finished with exit code 0</task-notification>",
		"<div> wrappers should be flex containers on every page of the dashboard",
	), SaidOpts{})
	want := []string{
		"use the smaller model for all subagents from now on please",
		"keep the helpers in this file private",
		"<div> wrappers should be flex containers on every page of the dashboard",
	}
	if len(got) != len(want) {
		t.Fatalf("got %q", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("turn %d = %q, want %q", i, got[i], want[i])
		}
	}
}

// Japanese and Chinese are written without spaces: a turn is one or two
// "words" to a tokenizer and still a full instruction.
func TestSaidKeepsTurnsInScriptsWithoutSpaces(t *testing.T) {
	got := saidTexts(userMsgs(
		"サブエージェントには常に小さいモデルを使ってください。大きいモデルは遅すぎます",
		"所有子代理都使用较小的模型，大模型太慢了",
		"はい", // too short to be a memory in any script
	), SaidOpts{})
	if len(got) != 2 {
		t.Fatalf("kept %d: %q", len(got), got)
	}
}

// A said record carries no file times: the edit the turn asked for must
// not mark the turn itself as needing verification.
func TestSaidCarriesNoFileTimes(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "src"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "src", "Header.tsx"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	recs := Said(userMsgs("make the header sticky in src/Header.tsx and keep the logo on the left side"),
		ExtractOpts{ProjectKey: "acme/api", SessionID: "s1", WorkspaceRoot: dir}, SaidOpts{})
	if len(recs) != 1 || len(recs[0].Paths) != 1 || len(recs[0].PathMtime) != 0 {
		t.Fatalf("said record: %+v", recs)
	}
}

// Without a mark of a brief, a long opening prompt is a person's, on any
// harness.
func TestSaidKeepsALongPlainOpeningPrompt(t *testing.T) {
	long := strings.Repeat("i want the upload screen to feel calmer than it does today, with fewer moving parts and one clear next step. ", 7)
	if len(long) <= saidClip {
		t.Fatal("fixture too short")
	}
	if got := saidTexts(userMsgs(long), SaidOpts{TapeStart: true}); len(got) != 1 {
		t.Fatalf("a long plain opener was dropped: %d", len(got))
	}
}
