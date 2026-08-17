package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"lossless/internal/claim"
	"lossless/internal/embed"
	"lossless/internal/projectkey"

	_ "modernc.org/sqlite"
)

type Store struct {
	Root     string
	DB       *sql.DB
	Embedder embed.Embedder
}

func Open(root string) (*Store, error) {
	if err := os.MkdirAll(filepath.Join(root, "export"), 0o755); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Join(root, "raw"), 0o755); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Join(root, "index"), 0o755); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Join(root, "spool"), 0o755); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", filepath.Join(root, "index", "claims.sqlite"))
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec(`PRAGMA journal_mode=WAL`); err != nil {
		_ = db.Close()
		return nil, err
	}
	s := &Store{Root: root, DB: db}
	if err := s.migrate(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error { return s.DB.Close() }

func (s *Store) migrate() error {
	_, err := s.DB.Exec(`
CREATE TABLE IF NOT EXISTS records (
  id TEXT PRIMARY KEY,
  type TEXT NOT NULL,
  project_key TEXT NOT NULL,
  workspace_root TEXT,
  harness TEXT NOT NULL,
  session_id TEXT NOT NULL,
  created_at TEXT NOT NULL,
  text TEXT NOT NULL,
  why TEXT,
  paths_json TEXT NOT NULL,
  symbols_json TEXT NOT NULL,
  path_mtime_json TEXT NOT NULL,
  status TEXT NOT NULL,
  supersedes TEXT,
  source TEXT NOT NULL,
  claim_hash TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_proj ON records(project_key, status);
CREATE INDEX IF NOT EXISTS idx_hash ON records(claim_hash, status);
CREATE INDEX IF NOT EXISTS idx_type_time ON records(project_key, status, type, created_at);
CREATE TABLE IF NOT EXISTS cursors (
  path TEXT PRIMARY KEY,
  offset INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS sessions (
  jsonl TEXT PRIMARY KEY,
  session_id TEXT NOT NULL,
  harness TEXT NOT NULL,
  workspace TEXT,
  project_key TEXT
);
CREATE TABLE IF NOT EXISTS path_postings (
  project_key TEXT NOT NULL,
  path_key TEXT NOT NULL,
  record_id TEXT NOT NULL,
  PRIMARY KEY (project_key, path_key, record_id)
);
CREATE TABLE IF NOT EXISTS symbol_postings (
  project_key TEXT NOT NULL,
  symbol TEXT NOT NULL,
  record_id TEXT NOT NULL,
  PRIMARY KEY (project_key, symbol, record_id)
);
CREATE VIRTUAL TABLE IF NOT EXISTS records_fts USING fts5(
  body,
  record_id UNINDEXED,
  tokenize = 'unicode61'
);
`)
	if err != nil {
		return err
	}
	_, _ = s.DB.Exec(`ALTER TABLE records ADD COLUMN transcript_ref_json TEXT`)
	if _, err := s.DB.Exec(`
CREATE TABLE IF NOT EXISTS claim_vectors (
  record_id TEXT PRIMARY KEY,
  project_key TEXT NOT NULL,
  model TEXT NOT NULL,
  dim INTEGER NOT NULL,
  vec BLOB NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_vec_proj ON claim_vectors(project_key, model);
CREATE TABLE IF NOT EXISTS actions (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  project_key TEXT NOT NULL,
  session_id TEXT NOT NULL,
  kind TEXT NOT NULL,
  claim_id TEXT,
  paths_json TEXT NOT NULL,
  tokens_json TEXT NOT NULL,
  at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_actions_sess ON actions(project_key, session_id, at);
`); err != nil {
		return err
	}
	return s.backfillIndex()
}

func (s *Store) Cursor(path string) int64 {
	var off int64
	err := s.DB.QueryRow(`SELECT offset FROM cursors WHERE path = ?`, path).Scan(&off)
	if err != nil {
		return 0
	}
	return off
}

func (s *Store) SetCursor(path string, offset int64) error {
	_, err := s.DB.Exec(`INSERT INTO cursors(path, offset) VALUES(?, ?)
		ON CONFLICT(path) DO UPDATE SET offset = excluded.offset`, path, offset)
	return err
}

func (s *Store) WriteClaim(rec claim.Record) (superseded string, err error) {
	if rec.ProjectKey != "" {
		rec.ProjectKey = projectkey.Normalize(rec.ProjectKey)
	}
	if rec.ID == "" {
		rec.ID = claim.NewID()
	}
	if rec.CreatedAt == "" {
		rec.CreatedAt = time.Now().UTC().Format(time.RFC3339)
	}
	if rec.Status == "" {
		rec.Status = "active"
	}
	if len(rec.Symbols) == 0 {
		rec.Symbols = claim.ExtractSymbols(rec.Text, rec.Paths)
	}
	if rec.ClaimHash == "" {
		rec.ClaimHash = claim.Hash(rec.ProjectKey, rec.Type, rec.Text)
	}
	var existing string
	_ = s.DB.QueryRow(
		`SELECT id FROM records WHERE claim_hash = ? AND status = 'active' AND id != ?`,
		rec.ClaimHash, rec.ID,
	).Scan(&existing)
	if existing != "" {
		if _, err := s.DB.Exec(`UPDATE records SET status = 'superseded' WHERE id = ?`, existing); err != nil {
			return "", err
		}
		rec.Supersedes = existing
		if err := s.rewriteStatus(existing, "superseded"); err != nil {
			return "", err
		}
		_ = s.DeleteVector(existing)
		superseded = existing
	}
	if err := s.writeFile(rec); err != nil {
		return "", err
	}
	if err := s.upsertRow(rec); err != nil {
		return "", err
	}
	if rec.Status == "active" {
		s.embedClaim(rec.ID, rec.ProjectKey, rec.Text, rec.Symbols)
	} else {
		_ = s.DeleteVector(rec.ID)
	}
	return superseded, nil
}

const claimCols = `id, type, project_key, workspace_root, harness, session_id, created_at, text, why,
		paths_json, symbols_json, path_mtime_json, status, supersedes, source, claim_hash, transcript_ref_json`

func (s *Store) Get(id string) (claim.Record, bool) {
	rec, err := s.scan(`SELECT `+claimCols+` FROM records WHERE id = ?`, id)
	if err != nil {
		return claim.Record{}, false
	}
	return rec, true
}

func (s *Store) ListActive(project string) ([]claim.Record, error) {
	rows, err := s.DB.Query(`SELECT `+claimCols+` FROM records WHERE project_key = ? AND status = 'active'`, projectkey.Normalize(project))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []claim.Record
	for rows.Next() {
		r, err := scanRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) rawDir(project string, t time.Time) string {
	month := t.UTC().Format("2006-01")
	return filepath.Join(s.Root, "raw", projectkey.Encode(project), month)
}

func (s *Store) RawPath(project, sessionID string, t time.Time) string {
	return filepath.Join(s.rawDir(project, t), sanitize(sessionID)+".jsonl")
}

// LiveRawPath is the uncompressed file we should append to. If the base
// session file is already sealed (.jsonl.zst), this is session.partN.jsonl.
func (s *Store) LiveRawPath(project, sessionID string, t time.Time) string {
	dir := s.rawDir(project, t)
	base := sanitize(sessionID)
	live := filepath.Join(dir, base+".jsonl")
	if fileExists(live) || !fileExists(live+".zst") {
		return live
	}
	for n := 2; n < 1000; n++ {
		part := filepath.Join(dir, fmt.Sprintf("%s.part%d.jsonl", base, n))
		if fileExists(part) || !fileExists(part+".zst") {
			return part
		}
	}
	return filepath.Join(dir, base+".part1000.jsonl")
}

func fileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

func (s *Store) ManualRawPath(t time.Time) string {
	day := t.UTC().Format("2006-01")
	dir := filepath.Join(s.Root, "raw", "manual", day)
	return filepath.Join(dir, "remember.jsonl")
}

func (s *Store) writeFile(rec claim.Record) error {
	dir := filepath.Join(s.Root, "export", projectkey.Encode(rec.ProjectKey))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	dest := filepath.Join(dir, rec.ID+".md")
	tmp := dest + ".tmp"
	body := serialize(rec)
	if err := os.WriteFile(tmp, []byte(body), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, dest)
}

func (s *Store) rewriteStatus(id, status string) error {
	rec, ok := s.Get(id)
	if !ok {
		return nil
	}
	rec.Status = status
	if err := s.writeFile(rec); err != nil {
		return err
	}
	return s.upsertRow(rec)
}

func (s *Store) upsertRow(rec claim.Record) error {
	pj, _ := json.Marshal(rec.Paths)
	sj, _ := json.Marshal(rec.Symbols)
	mj, _ := json.Marshal(rec.PathMtime)
	if rec.PathMtime == nil {
		mj = []byte("{}")
	}
	if rec.Paths == nil {
		pj = []byte("[]")
	}
	if rec.Symbols == nil {
		sj = []byte("[]")
	}
	ref := []byte("null")
	if rec.TranscriptRef != nil {
		ref, _ = json.Marshal(rec.TranscriptRef)
	}
	_, err := s.DB.Exec(`INSERT INTO records(
		id, type, project_key, workspace_root, harness, session_id, created_at, text, why,
		paths_json, symbols_json, path_mtime_json, status, supersedes, source, claim_hash, transcript_ref_json
	) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
	ON CONFLICT(id) DO UPDATE SET
		status=excluded.status, supersedes=excluded.supersedes, text=excluded.text,
		paths_json=excluded.paths_json, symbols_json=excluded.symbols_json,
		path_mtime_json=excluded.path_mtime_json, transcript_ref_json=excluded.transcript_ref_json`,
		rec.ID, rec.Type, rec.ProjectKey, rec.WorkspaceRoot, rec.Harness, rec.SessionID, rec.CreatedAt,
		rec.Text, rec.Why, string(pj), string(sj), string(mj), rec.Status, rec.Supersedes, rec.Source, rec.ClaimHash, string(ref))
	if err != nil {
		return err
	}
	return s.reindex(rec)
}

func (s *Store) scan(q string, args ...any) (claim.Record, error) {
	row := s.DB.QueryRow(q, args...)
	return scanRow(row)
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanRow(row rowScanner) (claim.Record, error) {
	var r claim.Record
	var ws, why, sup, ref sql.NullString
	var pj, sj, mj string
	err := row.Scan(&r.ID, &r.Type, &r.ProjectKey, &ws, &r.Harness, &r.SessionID, &r.CreatedAt, &r.Text, &why,
		&pj, &sj, &mj, &r.Status, &sup, &r.Source, &r.ClaimHash, &ref)
	if err != nil {
		return r, err
	}
	r.WorkspaceRoot = ws.String
	r.Why = why.String
	r.Supersedes = sup.String
	_ = json.Unmarshal([]byte(pj), &r.Paths)
	_ = json.Unmarshal([]byte(sj), &r.Symbols)
	_ = json.Unmarshal([]byte(mj), &r.PathMtime)
	if ref.String != "" && ref.String != "null" {
		var tr claim.TranscriptRef
		if json.Unmarshal([]byte(ref.String), &tr) == nil && tr.SessionID != "" {
			r.TranscriptRef = &tr
		}
	}
	return r, nil
}

func serialize(rec claim.Record) string {
	var b strings.Builder
	b.WriteString("---\n")
	fmt.Fprintf(&b, "id: %q\n", rec.ID)
	fmt.Fprintf(&b, "type: %s\n", rec.Type)
	fmt.Fprintf(&b, "project_key: %s\n", rec.ProjectKey)
	fmt.Fprintf(&b, "harness: %s\n", rec.Harness)
	fmt.Fprintf(&b, "session_id: %q\n", rec.SessionID)
	fmt.Fprintf(&b, "created_at: %s\n", rec.CreatedAt)
	fmt.Fprintf(&b, "status: %s\n", rec.Status)
	fmt.Fprintf(&b, "source: %s\n", rec.Source)
	fmt.Fprintf(&b, "claim_hash: %s\n", rec.ClaimHash)
	if rec.Supersedes != "" {
		fmt.Fprintf(&b, "supersedes: %s\n", rec.Supersedes)
	}
	pj, _ := json.Marshal(rec.Paths)
	fmt.Fprintf(&b, "paths: %s\n", pj)
	if rec.TranscriptRef != nil {
		rj, _ := json.Marshal(rec.TranscriptRef)
		fmt.Fprintf(&b, "transcript_ref: %s\n", rj)
	}
	b.WriteString("---\n")
	b.WriteString(rec.Text)
	b.WriteByte('\n')
	return b.String()
}

func sanitize(id string) string {
	id = strings.ReplaceAll(id, "/", "_")
	id = strings.ReplaceAll(id, string(filepath.Separator), "_")
	id = strings.ReplaceAll(id, "..", "_")
	id = strings.Trim(id, ". ")
	if id == "" || id == "." || id == ".." {
		return "unknown"
	}
	return id
}
