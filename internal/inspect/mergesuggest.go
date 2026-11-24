package inspect

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"lossless/internal/projectkey"
	"lossless/internal/store"
)

// MergeSuggestion is a project key whose records were written from
// checkouts that now resolve to another owner/repo: a path-hash key from
// before the repo had a remote, or the old name of a renamed remote.
type MergeSuggestion struct {
	From, Into string
	Active     int // active records under From
	// Total is every record under From; Located is how many of them
	// name a workspace that still resolves to some project.
	Total, Located int
	Share          float64 // of Located, the part that now resolves to Into
}

// mergeShare is how much of a key's located records must now resolve to
// one owner/repo before the key is suggested into it. At least
// mergeLocated of the key's records must be located at all: said records
// read back from old tapes name no workspace, and one note written from
// another checkout must not speak for fifty records that name none.
const (
	mergeShare   = 0.8
	mergeLocated = 0.5
)

// SuggestMerges reads every record's workspace, resolves each workspace
// as it is today and suggests a merge for each key whose records mostly
// resolve to some other owner/repo. resolve is projectkey.FromWorkspace
// outside tests.
//
// A workspace that is gone (a removed worktree) resolves through its
// nearest existing parent, but only when that parent sits inside a git
// checkout other than the home directory: a gone ~/developer/foo must
// not read as a dotfiles repo at ~. Suggestions only ever point at an
// owner/repo key, never at another path-hash key.
func SuggestMerges(st *store.Store, resolve func(string) string) ([]MergeSuggestion, error) {
	rows, err := st.DB.Query(`SELECT project_key, COALESCE(workspace_root, ''), count(*), sum(status = 'active')
		FROM records GROUP BY 1, 2`)
	if err != nil {
		return nil, err
	}
	type cell struct {
		ws        string
		n, active int
	}
	by := map[string][]cell{}
	for rows.Next() {
		var key string
		var c cell
		if err := rows.Scan(&key, &c.ws, &c.n, &c.active); err != nil {
			rows.Close()
			return nil, err
		}
		by[key] = append(by[key], c)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	home, _ := os.UserHomeDir()
	resolved := map[string]string{}
	resolveWS := func(ws string) string {
		if k, ok := resolved[ws]; ok {
			return k
		}
		k := resolveExisting(ws, home, resolve)
		resolved[ws] = k
		return k
	}

	var out []MergeSuggestion
	for key, cells := range by {
		located, active, total := 0, 0, 0
		votes := map[string]int{}
		for _, c := range cells {
			active += c.active
			total += c.n
			if c.ws == "" {
				continue
			}
			k := resolveWS(c.ws)
			if k == "" {
				continue
			}
			located += c.n
			votes[k] += c.n
		}
		if located == 0 || active == 0 || float64(located) < mergeLocated*float64(total) {
			continue
		}
		best, bestN := "", 0
		for k, n := range votes {
			if n > bestN || n == bestN && k < best {
				best, bestN = k, n
			}
		}
		share := float64(bestN) / float64(located)
		if best == "" || best == key || strings.HasPrefix(best, "path-") || share < mergeShare {
			continue
		}
		out = append(out, MergeSuggestion{From: key, Into: best, Active: active, Total: total, Located: located, Share: share})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Active != out[j].Active {
			return out[i].Active > out[j].Active
		}
		return out[i].From < out[j].From
	})
	return out, nil
}

// resolveExisting is resolve on ws, or on its nearest existing parent
// when ws is gone. It returns "" when nothing reliable is left.
func resolveExisting(ws, home string, resolve func(string) string) string {
	ws = filepath.Clean(ws)
	if !filepath.IsAbs(ws) {
		return ""
	}
	if fi, err := os.Stat(ws); err == nil && fi.IsDir() {
		return resolve(ws)
	}
	for p := filepath.Dir(ws); p != filepath.Dir(p); p = filepath.Dir(p) {
		if fi, err := os.Stat(p); err != nil || !fi.IsDir() {
			continue
		}
		top := gitTop(p)
		if top == "" || sameDir(top, home) {
			return ""
		}
		return resolve(p)
	}
	return ""
}

// gitTop is the checkout root holding dir, found by walking up to a .git
// entry (a dir in a clone, a file in a worktree). No git process runs.
func gitTop(dir string) string {
	for p := dir; ; p = filepath.Dir(p) {
		if _, err := os.Lstat(filepath.Join(p, ".git")); err == nil {
			return p
		}
		if p == filepath.Dir(p) {
			return ""
		}
	}
}

func sameDir(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	ra, err1 := filepath.EvalSymlinks(a)
	rb, err2 := filepath.EvalSymlinks(b)
	if err1 != nil || err2 != nil {
		return filepath.Clean(a) == filepath.Clean(b)
	}
	return ra == rb
}

// DefaultResolve is how the CLI resolves a workspace.
var DefaultResolve = projectkey.FromWorkspace
