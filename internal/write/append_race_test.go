package write

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

// Two concurrent retries with the same X-Prev-Offset must not both pass
// the conflict check: one acks, one 409s, the tape holds the line once.
func TestAppendConcurrentSamePrevOffAppendsOnce(t *testing.T) {
	st := tmpStore(t)
	for i := 0; i < 60; i++ {
		session := fmt.Sprintf("race-%d", i)
		line := fmt.Sprintf(`{"type":"message","role":"user","content":"turn %d"}`, i) + "\n"
		req := AppendRequest{
			Project: "acme/api", Harness: "grok", SessionID: session,
			Client: "c1", PrevOff: 0, Body: []byte(line),
		}
		var wg sync.WaitGroup
		results := make([]AppendResult, 2)
		for j := 0; j < 2; j++ {
			wg.Add(1)
			go func(j int) {
				defer wg.Done()
				out, err := Append(st, req)
				if err != nil {
					t.Errorf("append: %v", err)
					return
				}
				results[j] = out
			}(j)
		}
		wg.Wait()
		acks := 0
		for _, r := range results {
			if !r.Conflict && !r.Noop {
				acks++
			}
		}
		if acks != 1 {
			t.Fatalf("iter %d: %d acks, want 1 (results %+v)", i, acks, results)
		}
		raw, err := os.ReadFile(st.LiveRawPath("acme/api", session, time.Now()))
		if err != nil {
			t.Fatal(err)
		}
		if n := strings.Count(string(raw), fmt.Sprintf("turn %d", i)); n != 1 {
			t.Fatalf("iter %d: line on tape %d times", i, n)
		}
	}
}
