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

// saidRawMax bounds the turn a said record is read from. Nobody types
// more than this; a larger turn is a paste or a dump.
const saidRawMax = 64 << 10

// saidSource is the turn as the user typed it, or false when part of it
// may not be theirs.
//
// A said record tells a later agent "the user typed this", so the rule is
// deny by default: after the two things a harness is known to wrap the
// user's own words in (an editor's context blocks in front, the mid-turn
// delivery wrapper), any markup left anywhere in the turn means a tool, a
// hook, or a paste is in it, and the turn is not stored as said. Nothing
// is cut out of the middle of a turn: text inside a block can contain the
// block's own closing tag, so no cut is safe. It reads the whole turn,
// not the long-turn clip, where a tag can be missing.
func saidSource(m Message) (string, bool) {
	raw := m.Text
	if m.full != "" {
		raw = m.full
	}
	if len(raw) > saidRawMax {
		return "", false
	}
	raw = stripContextLead(userQuery(raw))
	if SaidMarkedUp(raw) {
		return "", false
	}
	return stripNonProse(raw), true
}

var (
	// closingTagRE is any closing tag, whatever its name is made of. A
	// closed block is not typing, and a closing tag whose opening tag is
	// gone still ends one.
	closingTagRE = regexp.MustCompile(`</[a-z][^\s<>]*\s*>`)
	// harnessOpenRE is an opening tag shaped like the ones harnesses and
	// tools write: a name with a hyphen, an underscore, or a colon in it
	// (system-reminder, pasted_content, ns:tag), or one of the plain
	// names they use. It costs a typed "<your_key>"; "<div>" and
	// "Map<string, number>" are untouched.
	harnessOpenRE = regexp.MustCompile(`<(?:[a-z][a-z0-9]*[-_:][^\s<>]*|skills?\b|summary\b|result\b|event\b|output\b|status\b)`)
	// declRE is a comment, a declaration, or a processing instruction.
	declRE = regexp.MustCompile(`<[!?]`)
	// contextLeadRE is one editor context block at the very front, in
	// exactly the form the editor integration writes: one tag, text with
	// no angle bracket in it, the same tag closed. A block whose text
	// holds any markup does not match, and the turn is then turned away
	// for the tags it still carries.
	contextLeadRE = regexp.MustCompile(`^\s*<(ide_opened_file|ide_selection)(?:\s[^<>]*)?>[^<]*</(ide_opened_file|ide_selection)>`)
	// userQueryRE is the one wrapper a harness puts around a message it
	// delivered mid-turn, anchored to the start of the turn, around text
	// with no angle bracket in it. The same tags further into a turn are
	// part of whatever the turn quotes.
	userQueryRE = regexp.MustCompile(`^\s*The user sent a message while you were working:\s*<user_query>([^<]*)</user_query>`)
)

// SaidMarkedUp reports markup that is not the user's typing: any closing
// tag, any harness-shaped opening tag, any comment or declaration. "<div>
// wrappers should be flex" and "Map<string, number>" are typing. The
// reader applies it too, so a row an older version stored is judged by
// the same rule as a turn written today.
func SaidMarkedUp(s string) bool {
	low := strings.ToLower(s)
	return closingTagRE.MatchString(low) || harnessOpenRE.MatchString(low) || declRE.MatchString(low)
}

// stripContextLead removes the context blocks an IDE integration puts in
// front of what the user typed: the open file, the selection.
func stripContextLead(s string) string {
	for {
		m := contextLeadRE.FindStringSubmatchIndex(s)
		if m == nil || s[m[2]:m[3]] != s[m[4]:m[5]] {
			return s
		}
		s = s[m[1]:]
	}
}

// userQuery unwraps a message the harness delivered mid-turn ("The user
// sent a message while you were working: <user_query>…</user_query>").
func userQuery(s string) string {
	if m := userQueryRE.FindStringSubmatch(s); m != nil {
		return m[1]
	}
	return s
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
