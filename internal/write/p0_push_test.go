package write

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

// Home may accept less than the whole body (its body cap trims to the
// last newline). A 200 with a short accepted_through is a partial
// accept: the job must not be dropped, and the remainder must be kept
// for the next flush from the accepted offset.
func TestFlushPushPartialAcceptKeepsRemainder(t *testing.T) {
	var bodies []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b := make([]byte, 64)
		n, _ := r.Body.Read(b)
		bodies = append(bodies, string(b[:n]))
		prev := r.Header.Get("X-Prev-Offset")
		w.WriteHeader(http.StatusOK)
		if prev == "0" {
			// accepted only the first line "one\n" (4 bytes)
			_ = json.NewEncoder(w).Encode(AppendResult{AcceptedThrough: 4})
			return
		}
		_ = json.NewEncoder(w).Encode(AppendResult{AcceptedThrough: 8})
	}))
	t.Cleanup(srv.Close)
	t.Setenv("LOSSLESS_URL", srv.URL)
	home := t.TempDir()
	if _, err := WritePush(home, PushJob{SessionID: "s", PrevOff: 0, Body: "one\ntwo\n"}); err != nil {
		t.Fatal(err)
	}
	if _, err := FlushPush(home); err == nil {
		t.Fatal("partial accept must not read as full success")
	}
	files, _ := ListPush(home)
	if len(files) != 1 {
		t.Fatalf("remainder job missing: %v", files)
	}
	raw, _ := os.ReadFile(files[0])
	var job PushJob
	_ = json.Unmarshal(raw, &job)
	if job.PrevOff != 4 || job.Body != "two\n" {
		t.Fatalf("remainder not rewritten: %+v", job)
	}
	if n, err := FlushPush(home); err != nil || n != 1 {
		t.Fatalf("second flush n=%d err=%v", n, err)
	}
	if files, _ := ListPush(home); len(files) != 0 {
		t.Fatalf("job left after full accept: %v", files)
	}
}
