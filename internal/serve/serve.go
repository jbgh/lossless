package serve

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"context"

	"lossless/internal/claim"
	"lossless/internal/mcpserver"
	"lossless/internal/retrieve"
	"lossless/internal/store"
	"lossless/internal/version"
	"lossless/internal/watch"
	"lossless/internal/write"
)

const DefaultAddr = "127.0.0.1:7432"

type Options struct {
	Addr  string
	Token string
	Watch bool
}

func CheckAddr(addr, token string) error {
	if addr == "" {
		addr = DefaultAddr
	}
	if loopback(addr) {
		return nil
	}
	if strings.TrimSpace(token) == "" {
		return fmt.Errorf("refusing to listen on %s without a bearer token", addr)
	}
	return nil
}

func loopback(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		// ":7432" binds all interfaces
		if strings.HasPrefix(addr, ":") {
			return false
		}
		host = addr
	}
	if host == "" {
		return false
	}
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func Handler(st *store.Store, token string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		emb := st.EmbedderName()
		if emb == "" {
			emb = "none"
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "records": st.CountActive(), "embedder": emb, "version": version.Version})
	})
	mux.HandleFunc("/v1/ask", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var req retrieve.Request
		if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
			return
		}
		out, err := retrieve.Ask(st, req)
		if err != nil {
			if errors.Is(err, retrieve.ErrBadRequest) {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
				return
			}
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, out)
	})
	mux.HandleFunc("/v1/catch-up", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var req write.CatchUpRequest
		var body struct {
			PathToJSONL   string           `json:"path_to_jsonl"`
			JSONL         string           `json:"jsonl"`
			Project       string           `json:"project"`
			WorkspaceRoot string           `json:"workspace_root"`
			Harness       string           `json:"harness"`
			SessionID     string           `json:"session_id"`
			Source        string           `json:"source"`
			Messages      []map[string]any `json:"messages"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 8<<20)).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
			return
		}
		req.JSONL = body.JSONL
		if req.JSONL == "" {
			req.JSONL = body.PathToJSONL
		}
		req.Project = body.Project
		req.WorkspaceRoot = body.WorkspaceRoot
		req.Harness = body.Harness
		req.SessionID = body.SessionID
		req.Source = body.Source
		req.Messages = body.Messages
		out, err := write.CatchUp(st, req)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		go func() { _, _ = write.FlushPush(st.Root) }()
		if retrieve.CompactSource(req.Source) {
			go retrieve.RefreshActive(st, st.Root, req)
		}
		writeJSON(w, http.StatusOK, out)
	})
	mux.HandleFunc("/v1/remember", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var body struct {
			claim.Record
			Project string `json:"project"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 256<<10)).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
			return
		}
		rec := body.Record
		if rec.ProjectKey == "" {
			rec.ProjectKey = body.Project
		}
		out, err := write.Remember(st, rec)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, out)
	})
	mux.HandleFunc("/v1/append", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		body, err := io.ReadAll(io.LimitReader(r.Body, 32<<20))
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "read body"})
			return
		}
		prev, _ := strconv.ParseInt(r.Header.Get("X-Prev-Offset"), 10, 64)
		out, err := write.Append(st, write.AppendRequest{
			Project:   r.Header.Get("X-Project"),
			Harness:   r.Header.Get("X-Harness"),
			SessionID: r.Header.Get("X-Session"),
			Client:    r.Header.Get("X-Client"),
			Workspace: r.Header.Get("X-Workspace"),
			PrevOff:   prev,
			Body:      body,
		})
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		if out.Conflict {
			writeJSON(w, http.StatusConflict, out)
			return
		}
		writeJSON(w, http.StatusOK, out)
	})
	mux.Handle("/mcp", mcpserver.New(mcpserver.Local{Store: st}).HTTPHandler())
	mux.Handle("/mcp/", mcpserver.New(mcpserver.Local{Store: st}).HTTPHandler())
	mux.HandleFunc("/v1/records/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		id := strings.TrimPrefix(r.URL.Path, "/v1/records/")
		id = strings.Trim(id, "/")
		if id == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing id"})
			return
		}
		if !store.SafeRecordID(id) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid id"})
			return
		}
		rec, ok := st.View(id)
		if !ok {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
			return
		}
		st.RecordDwell(r.URL.Query().Get("project"), r.URL.Query().Get("session"), id)
		writeJSON(w, http.StatusOK, rec)
	})
	var h http.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, 32<<20)
		mux.ServeHTTP(w, r)
	})
	if strings.TrimSpace(token) != "" {
		h = withBearer(h, token)
	}
	return h
}

func ListenAndServe(addr, token string, st *store.Store) error {
	return Listen(Options{Addr: addr, Token: token}, st)
}

func Listen(opts Options, st *store.Store) error {
	addr := opts.Addr
	if addr == "" {
		addr = DefaultAddr
	}
	if err := CheckAddr(addr, opts.Token); err != nil {
		return err
	}
	if opts.Watch {
		wopts := watch.Defaults()
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go func() { _ = watch.Run(ctx, st, wopts) }()
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		if alreadyServing(addr) {
			return nil
		}
		return err
	}
	return http.Serve(ln, Handler(st, opts.Token))
}

func alreadyServing(addr string) bool {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	client := &http.Client{
		Timeout: 300 * time.Millisecond,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	res, err := client.Get("http://" + net.JoinHostPort(host, port) + "/health")
	if err != nil {
		return false
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return false
	}
	raw, err := io.ReadAll(io.LimitReader(res.Body, 4<<10))
	if err != nil {
		return false
	}
	return decodeHealth(raw)
}

// decodeHealth is true only for our /health JSON: {"ok":true,"records":N}.
// A bare {"ok":true} or a plain 200 is not enough to treat a port as ours.
func decodeHealth(raw []byte) bool {
	var body struct {
		OK      *bool `json:"ok"`
		Records *int  `json:"records"`
	}
	if json.Unmarshal(raw, &body) != nil {
		return false
	}
	return body.OK != nil && *body.OK && body.Records != nil
}

func bearerMatch(got, want string) bool {
	gh := sha256.Sum256([]byte(got))
	wh := sha256.Sum256([]byte(want))
	return subtle.ConstantTimeCompare(gh[:], wh[:]) == 1
}

func withBearer(next http.Handler, token string) http.Handler {
	want := []byte(token)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			next.ServeHTTP(w, r)
			return
		}
		got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !bearerMatch(got, string(want)) {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	_ = enc.Encode(v)
}
