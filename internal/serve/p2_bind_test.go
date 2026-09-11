package serve

import (
	"net"
	"net/http"
	"testing"
	"time"

	"lossless/internal/store"
)

// launchctl kickstart -k starts the new daemon while the old one is still
// draining the port. Thirteen "bind: address already in use" exits in the
// live log. Listen waits for the port instead of giving up.
func TestListenWaitsForPortToFree(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	holder, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := holder.Addr().String()
	errCh := make(chan error, 1)
	go func() { errCh <- Listen(Options{Addr: addr}, st) }()
	time.Sleep(600 * time.Millisecond)
	select {
	case err := <-errCh:
		t.Fatalf("gave up while the port was held: %v", err)
	default:
	}
	_ = holder.Close()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		res, err := http.Get("http://" + addr + "/health")
		if err == nil {
			res.Body.Close()
			if res.StatusCode == 200 {
				return
			}
		}
		select {
		case err := <-errCh:
			t.Fatal(err)
		default:
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("never healthy after the port freed")
}
