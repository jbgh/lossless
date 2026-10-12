package write

import (
	"encoding/json"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"lossless/internal/claim"
	"lossless/internal/gate"
	"lossless/internal/redact"
	"lossless/internal/store"
)

// A said record is one turn the user typed, word for word. Extract keeps
// a sentence only when a cue word marks it as a rule or a decision, and
// most of what a person asks for carries none ("use the smaller model for
// subagents", "the export should keep the column order people rely on"). The
// user types little and all of it is theirs, so every turn is kept and
// ask decides relevance.
const (
	saidMinLen   = 25
	saidMaxLen   = 1500 // longer is a pasted brief; extract still reads it
	saidClip     = 600
	saidMinWords = 4
	// saidMinUnspaced is the length, in letters, at which a turn in a
	// script written without spaces says as much as four words.
	saidMinUnspaced = 12
)

// SaidOpts are the two things Said cannot know from the messages alone.
type SaidOpts struct {
	// At is when the turn was typed. Nil or zero means now.
	At func(Message) time.Time
	// LeadSeen reports a turn opening already stored from two other
	// sessions: a template, not a person. Nil keeps every turn.
	LeadSeen func(lead string) bool
	// TapeStart says msgs begin at the start of the session's tape, so
	// its first user turn is the session's opening prompt.
	TapeStart bool
	// Interactive reports a turn whose line says a person is driving the
	// session (Claude Code's cli entrypoint). Its opening prompt is the
	// user's, however long. Nil: not known.
	Interactive func(Message) bool
	// Programmatic reports a turn whose line says a program started the
	// session (Claude Code's sdk entrypoints). Nobody typed its user
	// turns, and they are not stored as the user's words. Nil: not known.
	Programmatic func(Message) bool
	// Briefs remembers the opening briefs turned away, by text, so the
	// same brief met again in a re-copied tape is still a brief. Nil
	// keeps no memory.
	Briefs map[string]bool
}

// Said returns one record of type "said" per human-typed user turn.
func Said(msgs []Message, opts ExtractOpts, so SaidOpts) []claim.Record {
	var out []claim.Record
	seen := map[string]bool{}
	opening := so.TapeStart
	for _, m := range msgs {
		if m.Skip || m.Compact || m.Role != "user" {
			continue
		}
		first := opening
		opening = false
		// A said record tells a later agent "the user typed this". That
		// is only safe to say of text a person did type: not of a turn a
		// program wrote, and not of what a person pasted in from
		// somewhere else.
		if so.Programmatic != nil && so.Programmatic(m) {
			continue
		}
		src, ok := saidSource(m)
		if !ok {
			continue
		}
		src, ok = stripLeadBlocks(userQuery(src))
		if !ok {
			continue
		}
		text := strings.Join(strings.Fields(src), " ")
		// "[Image #1] make the header sticky like this" is the user's
		// words after the marker. A file mention stays: it is the
		// referent, and the path the record is found by.
		text = strings.TrimSpace(imageLeadRE.ReplaceAllString(text, ""))
		if !saidWorthy(text) {
			continue
		}
		if so.Briefs[text] {
			continue
		}
		// A child session opens with the brief its parent wrote. Where
		// the harness does not say who is driving, one mark of a brief on
		// a turn that long is enough for that turn. Length alone is not:
		// a person may open with a page of plain prose.
		driven := so.Interactive != nil && so.Interactive(m)
		if first && !driven && len(text) >= gate.BriefLen && gate.BriefSignals(text) >= 1 {
			if so.Briefs != nil {
				so.Briefs[text] = true
			}
			continue
		}
		if so.LeadSeen != nil && so.LeadSeen(SaidLead(text)) {
			continue
		}
		paths := redact.FilterPaths(claim.Uniq(findPaths(text)))
		text = clipSaid(text)
		if redact.ShouldDropClaim(text, paths) {
			continue
		}
		rec := makeRec("said", text, paths, m, opts)
		// No file times: they exist to flag a claim about a file as
		// [verify] once the file changes, and a turn asking for the
		// change would be flagged by the change it asked for.
		rec.PathMtime = map[string]int64{}
		if seen[rec.ClaimHash] {
			continue
		}
		seen[rec.ClaimHash] = true
		if so.At != nil {
			if at := so.At(m); !at.IsZero() {
				rec.CreatedAt = at.UTC().Format(time.RFC3339)
				rec.ID = claim.NewIDAt(at)
			}
		}
		out = append(out, rec)
	}
	return out
}

func saidWorthy(text string) bool {
	if len(text) < saidMinLen || len(text) > saidMaxLen || gate.MachineTurn(text) {
		return false
	}
	// "Let's only use the V4 models" is four words and a rule; "yes do
	// that" is not a memory. Writing without spaces between words
	// (Japanese, Chinese) is measured by its letters instead.
	toks := claim.Tokens(stripPaths(text))
	long := map[string]bool{}
	for _, t := range toks {
		if len([]rune(t)) > 3 {
			long[t] = true
		}
	}
	if (len(toks) < saidMinWords || len(long) < 2) && unspacedLetters(text) < saidMinUnspaced {
		return false
	}
	// A turn that only asks is not something to hold the agent to, and
	// neither is one that only steers this session ("don't push yet",
	// "never mind"). A request worded as a question ("can you give me
	// the choices as a list?") is.
	sents := splitSentences(text)
	if len(sents) == 0 {
		return false
	}
	for _, s := range sents {
		s = strings.TrimSpace(s)
		if gate.SessionOp(s) {
			continue
		}
		if !strings.HasSuffix(s, "?") || politeRequestRE.MatchString(s) {
			return true
		}
	}
	return false
}

var politeRequestRE = regexp.MustCompile(`(?i)^(?:ok |okay |and |also |so )?(?:can|could|would|will) (?:you|we)\b|^please\b`)

// imageLeadRE is the marker a harness puts before the user's words when
// they attach an image.
var imageLeadRE = regexp.MustCompile(`^(?:\[Image #?\d+\]\s*)+`)

// unspacedLetters counts the letters of scripts written without spaces
// between words: Han, Hiragana, Katakana, Thai.
func unspacedLetters(s string) int {
	n := 0
	for _, r := range s {
		if unicode.In(r, unicode.Han, unicode.Hiragana, unicode.Katakana, unicode.Thai) {
			n++
		}
	}
	return n
}

var leadBlockOpenRE = regexp.MustCompile(`^\s*<([a-z][\w-]*)(?:\s[^>]*)?>`)

// contextBlocks are the tags an IDE integration puts in front of what the
// user typed: the file open in the editor, the selection.
var contextBlocks = map[string]bool{"ide_opened_file": true, "ide_selection": true}

// stripLeadBlocks removes the context blocks in front of the user's
// words. It reports false for a turn that opens with any other closed
// tag block: that is a harness or a tool speaking in the user's place
// (a notification, a reminder, a hook's output), and what follows the
// block is not known to be the user's. A tag that is never closed is the
// user writing about markup ("<div> wrappers should be flex"), and stays.
func stripLeadBlocks(s string) (string, bool) {
	for i := 0; i < 8; i++ {
		m := leadBlockOpenRE.FindStringSubmatchIndex(s)
		if m == nil {
			break
		}
		name := s[m[2]:m[3]]
		closing := "</" + name + ">"
		end := strings.Index(s[m[1]:], closing)
		if end < 0 {
			break
		}
		if !contextBlocks[name] {
			return "", false
		}
		s = s[m[1]+end+len(closing):]
	}
	return s, true
}

// userQueryRE is the one wrapper a harness puts around a message it
// delivered mid-turn. It is anchored to the start of the turn: the same
// tags further in are part of whatever the turn quotes, and lifting their
// contents out would store quoted text as the user's own.
var userQueryRE = regexp.MustCompile(`(?s)^\s*The user sent a message while you were working:\s*<user_query>(.*?)</user_query>`)

// userQuery unwraps a message the harness delivered mid-turn ("The user
// sent a message while you were working: <user_query>…</user_query>").
func userQuery(s string) string {
	if m := userQueryRE.FindStringSubmatch(s); m != nil {
		return m[1]
	}
	return s
}

// saidRawMax bounds the turn a said record is read from. Nobody types
// more than this; a larger turn is a paste or a dump.
const saidRawMax = 64 << 10

// saidSource is the turn as the user typed it, without what they pasted
// and without the data in it (diffs, fenced code, JSON). It reads the
// whole turn, not the long-turn clip: a paste is cut out by its tags, and
// past a clip a tag can be missing.
func saidSource(m Message) (string, bool) {
	raw := m.Text
	if m.full != "" {
		raw = m.full
	}
	if len(raw) > saidRawMax {
		return "", false
	}
	return stripNonProse(stripPasted(raw)), true
}

const (
	pastedOpen  = "<pasted_content"
	pastedClose = "</pasted_content>"
)

// stripPasted removes what the user pasted from elsewhere. A paste is
// material the user handed over, and may hold instructions they did not
// write; only their own words around it are theirs.
//
// The pasted text can contain the tags themselves, so the tags are not
// matched in pairs: everything from the first opening tag to the last
// closing tag goes. A closing tag with no opening tag before it takes
// everything before it; an opening tag never closed takes the rest.
func stripPasted(s string) string {
	open := strings.Index(s, pastedOpen)
	last := strings.LastIndex(s, pastedClose)
	if open < 0 && last < 0 {
		return s
	}
	start, end := open, len(s)
	if first := strings.Index(s, pastedClose); open < 0 || (first >= 0 && first < open) {
		start = 0
	}
	if last >= start {
		end = last + len(pastedClose)
	}
	return s[:start] + " " + s[end:]
}

// saidLeadRunes is how much of a turn's opening the template check
// compares; saidLeadRead is how much it reads to get there (numbers fold
// to one character, so the opening is cut after folding).
const (
	saidLeadRunes = 40
	saidLeadRead  = 64
)

var digitsRE = regexp.MustCompile(`\d+`)

// SaidLead is the opening the template check compares: the first runes of
// the turn with every number folded, so "Check batch 4 workers" and
// "Check batch 12 workers" are one template.
func SaidLead(text string) string {
	r := []rune(text)
	if len(r) > saidLeadRead {
		r = r[:saidLeadRead]
	}
	r = []rune(digitsRE.ReplaceAllString(string(r), "#"))
	if len(r) > saidLeadRunes {
		r = r[:saidLeadRunes]
	}
	return string(r)
}

// loadOpenings maps each opening of a project's active said records to
// the sessions that used it. It reads only the openings, not the turns.
func loadOpenings(st *store.Store, project string) map[string]map[string]bool {
	out := map[string]map[string]bool{}
	rows, err := st.SaidOpenings(project, saidLeadRead)
	if err != nil {
		return out
	}
	for _, r := range rows {
		lead := SaidLead(r[1])
		if out[lead] == nil {
			out[lead] = map[string]bool{}
		}
		out[lead][r[0]] = true
	}
	return out
}

// templateLead reports an opening already stored from two sessions other
// than session.
func templateLead(st *store.Store, project, session, lead string) bool {
	sessions := loadOpenings(st, project)[lead]
	return len(sessions) > 2 || (len(sessions) == 2 && !sessions[session])
}

func clipSaid(s string) string {
	if len(s) <= saidClip {
		return s
	}
	n := saidClip
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return strings.TrimSpace(s[:n]) + "…"
}

var lineTimeRE = regexp.MustCompile(`"timestamp"\s*:\s*"([^"]+)"`)

// lineEntrypoint is how the session was started, as Claude Code stamps
// it on each line: "cli" is a person at a terminal, "sdk-…" a program.
// It is the line's own top-level field, read as JSON: the same letters
// inside the message text say nothing.
func lineEntrypoint(chunk string, rel int64) string {
	var top struct {
		Entrypoint string `json:"entrypoint"`
	}
	if err := json.Unmarshal([]byte(lineAt(chunk, rel)), &top); err != nil {
		return ""
	}
	return top.Entrypoint
}

// lineAt is the JSONL line that starts at rel in chunk.
func lineAt(chunk string, rel int64) string {
	if rel < 0 || rel >= int64(len(chunk)) {
		return ""
	}
	line := chunk[rel:]
	if i := strings.IndexByte(line, '\n'); i >= 0 {
		line = line[:i]
	}
	return line
}

// lineTime reads the harness timestamp of the JSONL line that starts at
// rel in chunk. A tape read again months later still dates its turns.
func lineTime(chunk string, rel int64) time.Time {
	line := lineAt(chunk, rel)
	if line == "" {
		return time.Time{}
	}
	m := lineTimeRE.FindStringSubmatch(line)
	if m == nil {
		return time.Time{}
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339} {
		if t, err := time.Parse(layout, m[1]); err == nil {
			return t
		}
	}
	return time.Time{}
}

// saidRun is how one caller wants said records written. The zero value
// is the live path: write every turn, ask the store about templates.
type saidRun struct {
	// skipKnown leaves a turn alone when the store holds it or ever did:
	// reading old tapes again must not churn records, nor bring back one
	// the user retired.
	skipKnown bool
	// dry counts what would be added and writes nothing.
	dry bool
	// leadSeen replaces the store's template check (one scan per turn)
	// for a caller that checks many turns.
	leadSeen func(session, lead string) bool
	// added holds the hashes this run has added, so a dry run and a real
	// one count a repeated turn once. Nil on the live path.
	added map[string]bool
	// briefs is SaidOpts.Briefs for a caller that reads a session's tape
	// in several pieces.
	briefs map[string]bool
	// onSeen is called for every qualifying turn, added or not, so the
	// caller's template books cover turns the store already holds.
	onSeen func(claim.Record)
}

// writeSaid stores the said records of one parsed chunk and reports how
// many turns qualified and how many were added. chunk is the text msgs
// were parsed from and base its offset in the tape; tapeStart says the
// chunk opens the session. It never fails the caller: the tape is
// written, and a turn missed here is read again by backfill.
func writeSaid(st *store.Store, msgs []Message, chunk string, base int64, opts ExtractOpts, fallback time.Time, tapeStart bool, run *saidRun) (turns, added int) {
	if run == nil {
		run = &saidRun{}
	}
	var openings map[string]map[string]bool
	recs := Said(msgs, opts, SaidOpts{
		TapeStart: tapeStart,
		Briefs:    run.briefs,
		Interactive: func(m Message) bool {
			return lineEntrypoint(chunk, m.Offset-base) == "cli"
		},
		Programmatic: func(m Message) bool {
			return strings.HasPrefix(lineEntrypoint(chunk, m.Offset-base), "sdk-")
		},
		At: func(m Message) time.Time {
			if t := lineTime(chunk, m.Offset-base); !t.IsZero() {
				return t
			}
			return fallback
		},
		LeadSeen: func(lead string) bool {
			if run.leadSeen != nil {
				return run.leadSeen(opts.SessionID, lead)
			}
			// One read of the project's openings per chunk, however many
			// turns the chunk holds.
			if openings == nil {
				openings = loadOpenings(st, opts.ProjectKey)
			}
			return len(openings[lead]) > 2 || (len(openings[lead]) == 2 && !openings[lead][opts.SessionID])
		},
	})
	for _, rec := range recs {
		turns++
		if run.onSeen != nil {
			run.onSeen(rec)
		}
		if run.added[rec.ClaimHash] || (run.skipKnown && st.KnownHash(rec.ClaimHash)) {
			continue
		}
		if !run.dry {
			if _, err := st.WriteClaim(rec); err != nil {
				continue
			}
		}
		if run.added != nil {
			run.added[rec.ClaimHash] = true
		}
		added++
	}
	return turns, added
}
