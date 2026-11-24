package write

import (
	"bufio"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"lossless/internal/claim"
	"lossless/internal/projectkey"
	"lossless/internal/store"
)

// BackfillResult counts one pass over the stored tapes.
type BackfillResult struct {
	Tapes int `json:"tapes"`
	Turns int `json:"turns"`
	Added int `json:"added"`
	// Skipped tapes belong to no session on record: sessions that prune
	// removed (test workspaces, misfiled streams) keep their tape.
	Skipped int `json:"skipped"`
}

var tapePartRE = regexp.MustCompile(`\.part\d+$`)

// Backfill reads the stored tapes again and adds what this version keeps
// that the version which wrote them did not: the user's turns, as said
// records. The tape is the corpus, so memory a later release learns to
// keep is not lost for the sessions before it. It is safe to run twice
// and beside the daemon: a turn the store already holds is left alone.
// project limits the pass to one owner/repo; dry counts without writing.
//
// A tape's records go under the key its folder names while the session
// still has a row there, and under the session's project once it has
// none: merge-project moves records and sessions and leaves the tapes
// where they were written.
func Backfill(st *store.Store, project string, dry bool) (BackfillResult, error) {
	var res BackfillResult
	want := ""
	if strings.TrimSpace(project) != "" {
		want = projectkey.Normalize(project)
	}
	root := filepath.Join(st.Root, "raw")
	projects, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return res, nil
		}
		return res, err
	}
	type tape struct {
		path, project, session, month string
		// folder is the key the tape's folder names. It differs from
		// project after merge-project, or when the session changed project.
		folder   string
		sess     store.Session
		onRecord bool
		mod      int64
		part     bool // a later part of a sealed session
	}
	var tapes []tape
	listed := map[string]bool{}
	for _, p := range projects {
		// manual/ holds remember lines: already records.
		if !p.IsDir() || p.Name() == "manual" {
			continue
		}
		folder := projectkey.Decode(p.Name())
		months, _ := os.ReadDir(filepath.Join(root, p.Name()))
		for _, m := range months {
			if !m.IsDir() {
				continue
			}
			dir := filepath.Join(root, p.Name(), m.Name())
			files, _ := os.ReadDir(dir)
			for _, f := range files {
				name := strings.TrimSuffix(f.Name(), ".zst")
				path := filepath.Join(dir, name)
				// A tape can be there both plain and sealed for a moment.
				if f.IsDir() || !strings.HasSuffix(name, ".jsonl") || listed[path] {
					continue
				}
				fi, err := f.Info()
				if err != nil {
					continue
				}
				listed[path] = true
				stem := strings.TrimSuffix(name, ".jsonl")
				session := tapePartRE.ReplaceAllString(stem, "")
				// The folder says where the tape was first written, and
				// its key stands while the session still has a row under
				// it (one session id can have tapes in two projects).
				// When no row carries it any more the session has moved:
				// merge-project moves records and sessions and leaves
				// tapes in place, so the newest row names the project.
				sess, ok := st.SessionByID(session)
				project := folder
				if own, has := st.SessionInProject(session, folder); has {
					sess = own
				} else if k := projectkey.Normalize(sess.Project); ok && k != "" {
					project = k
				}
				if want != "" && project != want {
					continue
				}
				tapes = append(tapes, tape{
					path: path, project: project, folder: folder, session: session, month: m.Name(),
					sess: sess, onRecord: ok, mod: fi.ModTime().UnixNano(), part: session != stem,
				})
			}
		}
	}
	// Oldest first, by month and then by last write: the template check
	// learns an opening from the first sessions that used it, and a
	// session's first tape comes before the ones that continue it. (Sealing
	// rewrites a file, so mtime alone can put last month's tape after this
	// month's.)
	sort.SliceStable(tapes, func(i, j int) bool {
		if tapes[i].month != tapes[j].month {
			return tapes[i].month < tapes[j].month
		}
		return tapes[i].mod < tapes[j].mod
	})

	// One pass over a project's said records instead of a scan per turn.
	// The dry run keeps the same books, so it counts what the real run adds.
	leads := map[string]map[string]map[string]bool{} // project -> opening -> sessions
	leadsOf := func(project string) map[string]map[string]bool {
		if m, ok := leads[project]; ok {
			return m
		}
		m := loadOpenings(st, project)
		leads[project] = m
		return m
	}
	added := map[string]bool{}
	begun := map[string]bool{}             // sessions whose first tape has been read
	briefs := map[string]map[string]bool{} // session -> opening briefs turned away
	for _, t := range tapes {
		// Bench and eval tapes imported into a live project stay raw, and
		// so does the tape of a session prune removed: only a session
		// still on record is read again.
		if !t.onRecord || claim.FixtureSession(t.session) {
			res.Skipped++
			continue
		}
		opts := ExtractOpts{ProjectKey: t.project, SessionID: t.session, Harness: t.sess.Harness, Source: "turn"}
		if briefs[t.session] == nil {
			briefs[t.session] = map[string]bool{}
		}
		byLead := leadsOf(t.project)
		run := &saidRun{
			skipKnown: true,
			knownAs:   t.folder,
			dry:       dry,
			added:     added,
			briefs:    briefs[t.session],
			leadSeen: func(session, lead string) bool {
				others := 0
				for s := range byLead[lead] {
					if s != session {
						others++
					}
				}
				return others >= 2
			},
			onSeen: func(rec claim.Record) {
				lead := SaidLead(rec.Text)
				if byLead[lead] == nil {
					byLead[lead] = map[string]bool{}
				}
				byLead[lead][rec.SessionID] = true
			},
		}
		fallback := fileModTime(t.path)
		// The session opens in its first tape. A later part, or the tape
		// a new month starts, continues it from offset zero.
		opening := !t.part && !begun[t.session]
		begun[t.session] = true
		err := eachTapeChunk(t.path, func(chunk string, base int64) {
			msgs, _ := ParseJSONL(chunk, base)
			turns, n := writeSaid(st, msgs, chunk, base, opts, fallback, opening, run)
			res.Turns += turns
			res.Added += n
			opening = false
		})
		if err == nil {
			res.Tapes++
		}
	}
	return res, nil
}

func fileModTime(path string) time.Time {
	for _, p := range []string{path, path + ".zst"} {
		if fi, err := os.Stat(p); err == nil {
			return fi.ModTime()
		}
	}
	return time.Time{}
}

// backfillChunk bounds how much of a tape is parsed at once. A session
// tape can be hundreds of megabytes.
const backfillChunk = 16 << 20

// eachTapeChunk streams a tape as runs of whole lines, with the offset
// of each run in the tape.
func eachTapeChunk(path string, fn func(chunk string, base int64)) error {
	r, err := openRaw(path)
	if err != nil {
		return err
	}
	defer r.Close()
	br := bufio.NewReaderSize(r, 1<<20)
	var b strings.Builder
	base := int64(0)
	flush := func() {
		if b.Len() == 0 {
			return
		}
		chunk := b.String()
		fn(chunk, base)
		base += int64(len(chunk))
		b.Reset()
	}
	for {
		line, err := br.ReadString('\n')
		if strings.HasSuffix(line, "\n") {
			b.WriteString(line)
			if b.Len() >= backfillChunk {
				flush()
			}
		}
		if err != nil {
			flush()
			if err == io.EOF {
				return nil
			}
			return err
		}
	}
}
