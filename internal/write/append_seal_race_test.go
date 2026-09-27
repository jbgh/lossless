package write

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// A seal unlinks the plaintext it compressed. A writer that opened the
// file before that unlink and only then took the lock would append into
// an unlinked inode and lose the line. The append path re-checks the
// link count once it holds the lock and reopens through LiveRawPath, so
// the line lands in the new part instead.
func TestAppendDuringSealDoesNotLoseTheLine(t *testing.T) {
	st := tmpStore(t)
	for i := 0; i < 40; i++ {
		session := fmt.Sprintf("seal-race-%d", i)
		line := fmt.Sprintf(`{"type":"message","role":"user","content":"turn %d"}`, i) + "\n"
		req := AppendRequest{
			Project: "acme/api", Harness: "grok", SessionID: session,
			Client: "c1", PrevOff: 0, Body: []byte(line),
		}
		if _, err := Append(st, req); err != nil {
			t.Fatal(err)
		}
		raw := st.LiveRawPath("acme/api", session, time.Now())

		// Hold the file open across the seal, the way a writer that
		// opened it just before would.
		stale, err := os.OpenFile(raw, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
		if err != nil {
			t.Fatal(err)
		}

		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			defer stale.Close()
			_, _ = SealRaw(raw)
		}()
		go func(i int, req AppendRequest, line string) {
			defer wg.Done()
			// The seal usually wins; either order must keep the line.
			next := req
			next.PrevOff = int64(len(line))
			next.Body = []byte(strings.Replace(line, "turn", "next turn", 1) + "\n")
			if _, err := Append(st, next); err != nil {
				t.Errorf("append: %v", err)
			}
		}(i, req, line)
		wg.Wait()

		// The line must be readable from the tape, sealed or not, and
		// never only from a file nothing points at.
		var seen []string
		dir := filepath.Dir(raw)
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range entries {
			name := e.Name()
			if !strings.HasPrefix(name, session) {
				continue
			}
			// ReadRaw takes the plaintext path and finds the .zst
			// itself; handing it a .zst returns compressed bytes.
			body, err := ReadRaw(strings.TrimSuffix(filepath.Join(dir, name), ".zst"))
			if err != nil {
				continue
			}
			seen = append(seen, string(body))
		}
		joined := strings.Join(seen, "")
		if !strings.Contains(joined, fmt.Sprintf("turn %d", i)) {
			t.Fatalf("iter %d: first line lost, tape holds %q", i, joined)
		}
		if !strings.Contains(joined, "next turn") {
			t.Fatalf("iter %d: line appended during the seal lost, tape holds %q", i, joined)
		}
	}
}
