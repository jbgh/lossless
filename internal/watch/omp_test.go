package watch

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"lossless/internal/harness"
	"lossless/internal/store"
)

// omp sessions are tailed from <agent dir>/sessions/<bucket>, and a
// session's artifact directory (subagent transcripts, bash logs) is not a
// tape.
func TestDiscoverAndTickOmpSessions(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("OMP_HOME", "")
	t.Setenv("PI_CONFIG_DIR", "")
	t.Setenv("PI_CODING_AGENT_DIR", "")

	const ws = "/Users/jay/dev/api"
	const sid = "01a130e9-6160-70f1-915d-4a727bf69ef9"
	root := filepath.Join(home, ".omp", "agent", "sessions")
	bucket := filepath.Join(root, harness.OmpSessionSlug(ws))
	stem := "2026-10-12T16-19-23-616Z_" + sid
	tape := filepath.Join(bucket, stem+".jsonl")
	if err := os.MkdirAll(filepath.Dir(tape), 0o755); err != nil {
		t.Fatal(err)
	}
	body := `{"type":"title","v":1,"title":"t","pad":"   "}` + "\n" +
		`{"type":"session","version":3,"id":"` + sid + `","cwd":"` + ws + `"}` + "\n" +
		`{"type":"message","id":"c0","parentId":null,"message":{"role":"user","content":[{"type":"text","text":"add rate limiting"}]}}` + "\n" +
		`{"type":"message","id":"b2","parentId":"c0","message":{"role":"assistant","content":[{"type":"text","text":"We decided to use jose, not jsonwebtoken, for Edge in src/middleware/auth.ts."}]}}` + "\n"
	if err := os.WriteFile(tape, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	// A subagent transcript sits in the artifact directory named after the
	// session.
	artifact := filepath.Join(bucket, stem, "Explore.jsonl")
	if err := os.MkdirAll(filepath.Dir(artifact), 0o755); err != nil {
		t.Fatal(err)
	}
	dump := `{"type":"session","version":3,"id":"sub","cwd":"` + ws + `"}` + "\n" +
		`{"type":"message","message":{"role":"user","content":[{"type":"text","text":"READ-ONLY: do not push, edit, or merge. jose-subagent-poison."}]}}` + "\n"
	if err := os.WriteFile(artifact, []byte(dump), 0o644); err != nil {
		t.Fatal(err)
	}

	found := Discover(Options{OmpRoot: root}, nil)
	var omp []Target
	for _, f := range found {
		if f.Harness == "omp" {
			omp = append(omp, f)
		}
	}
	if len(omp) != 1 {
		t.Fatalf("omp targets %d: %+v", len(omp), found)
	}
	if omp[0].JSONL != tape || omp[0].SessionID != sid || omp[0].Workspace != ws {
		t.Fatalf("omp target %+v", omp[0])
	}

	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	res, err := Tick(st, Options{OmpRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	if res.CatchUps != 1 {
		t.Fatalf("catch_ups=%d (want the tape only): %+v", res.CatchUps, res)
	}
	sess, err := st.ListSessions()
	if err != nil {
		t.Fatal(err)
	}
	if len(sess) != 1 || sess[0].Harness != "omp" || sess[0].SessionID != sid || sess[0].Workspace != ws {
		t.Fatalf("stored sessions %+v", sess)
	}
	blob := tapeBlob(t, st)
	if !strings.Contains(blob, "jose, not jsonwebtoken") {
		t.Fatalf("tape missing: %s", blob)
	}
	if strings.Contains(blob, "subagent-poison") {
		t.Fatalf("artifact transcript ingested: %s", blob)
	}
}

func tapeBlob(t *testing.T, st *store.Store) string {
	t.Helper()
	var b strings.Builder
	err := filepath.WalkDir(filepath.Join(st.Root, "raw"), func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		b.Write(raw)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return b.String()
}
