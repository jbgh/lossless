package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
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
	writeMu  sync.Mutex
}

func Open(root string) (*Store, error) {
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, err
	}
	for _, sub := range []string{"export", "raw", "index", "spool"} {
		if err := os.MkdirAll(filepath.Join(root, sub), 0o700); err != nil {
			return nil, err
		}
	}
	dbPath := filepath.Join(root, "index", "claims.sqlite")
	db, err := sql.Open("sqlite", sqliteURI(dbPath))
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(4)
	db.SetMaxIdleConns(2)
	s := &Store{Root: root, DB: db}
	if err := s.migrate(); err != nil {
		_ = db.Close()
		return nil, err
	}
	_ = os.Chmod(dbPath, 0o600)
	return s, nil
}

func (s *Store) Close() error { return s.DB.Close() }

func sqliteURI(path string) string {
	q := make(url.Values)
	q.Add("_pragma", "busy_timeout(10000)")
	q.Add("_pragma", "journal_mode(WAL)")
	q.Add("_pragma", "synchronous(NORMAL)")
	u := url.URL{Scheme: "file", Path: path, RawQuery: q.Encode()}
	return u.String()
}

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
	if err := s.collapseDuplicateActive(); err != nil {
		return err
	}
	if _, err := s.DB.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_hash_active ON records(claim_hash) WHERE status = 'active'`); err != nil {
		return err
	}
	return s.backfillIndex()
}

func (s *Store) collapseDuplicateActive() error {
	rows, err := s.DB.Query(`SELECT claim_hash FROM records WHERE status = 'active' GROUP BY claim_hash HAVING COUNT(*) > 1`)
	if err != nil {
		return err
	}
	var hashes []string
	for rows.Next() {
		var h string
		if err := rows.Scan(&h); err != nil {
			_ = rows.Close()
			return err
		}
		hashes = append(hashes, h)
	}
	_ = rows.Close()
	for _, h := range hashes {
		var keep string
		if err := s.DB.QueryRow(`SELECT id FROM records WHERE claim_hash = ? AND status = 'active' ORDER BY created_at DESC, id DESC LIMIT 1`, h).Scan(&keep); err != nil {
			return err
		}
		if _, err := s.DB.Exec(`UPDATE records SET status = 'superseded' WHERE claim_hash = ? AND status = 'active' AND id != ?`, h, keep); err != nil {
			return err
		}
	}
	return nil
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
	return withBusyRetry(func() error {
		_, err := s.DB.Exec(`INSERT INTO cursors(path, offset) VALUES(?, ?)
		ON CONFLICT(path) DO UPDATE SET offset = excluded.offset`, path, offset)
		return err
	})
}

func (s *Store) WriteClaim(rec claim.Record) (superseded string, err error) {
	if rec.ProjectKey != "" {
		rec.ProjectKey = projectkey.Normalize(rec.ProjectKey)
	}
	if rec.ID == "" {
		rec.ID = claim.NewID()
	}
	if !SafeRecordID(rec.ID) {
		return "", fmt.Errorf("invalid record id")
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
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	err = withBusyRetry(func() error {
		var e error
		superseded, e = s.writeClaimOnce(rec)
		return e
	})
	if err != nil {
		return "", err
	}
	_ = s.reindex(rec)
	if superseded != "" {
		rec.Supersedes = superseded
		_ = s.rewriteStatus(superseded, "superseded")
		_ = s.DeleteVector(superseded)
	}
	if err := s.writeFile(rec); err != nil {
		return superseded, err
	}
	if rec.Status == "active" {
		s.embedClaim(rec.ID, rec.ProjectKey, rec.Text, rec.Symbols)
	} else {
		_ = s.DeleteVector(rec.ID)
	}
	return superseded, nil
}

func isBusy(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "SQLITE_BUSY") || strings.Contains(s, "database is locked")
}

func withBusyRetry(fn func() error) error {
	var err error
	for i := 0; i < 32; i++ {
		err = fn()
		if err == nil || !isBusy(err) {
			return err
		}
		time.Sleep(time.Duration(8+i*4) * time.Millisecond)
	}
	return err
}

func (s *Store) writeClaimOnce(rec claim.Record) (superseded string, err error) {
	tx, err := s.DB.Begin()
	if err != nil {
		return "", err
	}
	var existing string
	_ = tx.QueryRow(
		`SELECT id FROM records WHERE claim_hash = ? AND status = 'active' AND id != ?`,
		rec.ClaimHash, rec.ID,
	).Scan(&existing)
	if existing != "" {
		if _, err := tx.Exec(`UPDATE records SET status = 'superseded' WHERE id = ?`, existing); err != nil {
			_ = tx.Rollback()
			return "", err
		}
		rec.Supersedes = existing
		superseded = existing
	}
	if err := s.upsertRowTx(tx, rec); err != nil {
		_ = tx.Rollback()
		if strings.Contains(err.Error(), "UNIQUE constraint failed") && strings.Contains(err.Error(), "claim_hash") {
			return "", nil
		}
		return "", err
	}
	if err := tx.Commit(); err != nil {
		return "", err
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

func (s *Store) ProjectHasLiveWork(project string) bool {
	if s == nil || s.DB == nil {
		return false
	}
	rows, err := s.DB.Query(`SELECT DISTINCT session_id FROM records WHERE project_key = ? AND status = 'active'`, projectkey.Normalize(project))
	if err != nil {
		return false
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return false
		}
		if !claim.FixtureSession(id) {
			return true
		}
	}
	return false
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
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	dest := filepath.Join(dir, rec.ID+".md")
	tmp := dest + ".tmp"
	body := serialize(rec)
	if err := os.WriteFile(tmp, []byte(body), 0o600); err != nil {
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

type dbExec interface {
	Exec(query string, args ...any) (sql.Result, error)
}

func (s *Store) upsertRow(rec claim.Record) error {
	if err := upsertRecord(s.DB, rec); err != nil {
		return err
	}
	return s.reindex(rec)
}

func (s *Store) upsertRowTx(tx *sql.Tx, rec claim.Record) error {
	return upsertRecord(tx, rec)
}

func upsertRecord(e dbExec, rec claim.Record) error {
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
	_, err := e.Exec(`INSERT INTO records(
		id, type, project_key, workspace_root, harness, session_id, created_at, text, why,
		paths_json, symbols_json, path_mtime_json, status, supersedes, source, claim_hash, transcript_ref_json
	) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
	ON CONFLICT(id) DO UPDATE SET
		status=excluded.status, supersedes=excluded.supersedes, text=excluded.text,
		paths_json=excluded.paths_json, symbols_json=excluded.symbols_json,
		path_mtime_json=excluded.path_mtime_json, transcript_ref_json=excluded.transcript_ref_json`,
		rec.ID, rec.Type, rec.ProjectKey, rec.WorkspaceRoot, rec.Harness, rec.SessionID, rec.CreatedAt,
		rec.Text, rec.Why, string(pj), string(sj), string(mj), rec.Status, rec.Supersedes, rec.Source, rec.ClaimHash, string(ref))
	return err
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

var recordIDRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

// SafeRecordID is the write and lookup gate for claim IDs used as filenames.
func SafeRecordID(id string) bool {
	return recordIDRe.MatchString(id)
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
