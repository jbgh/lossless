package mcpserver

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"lossless/internal/claim"
	"lossless/internal/retrieve"
	"lossless/internal/store"
	"lossless/internal/write"
)

// Backend is the store-facing API MCP tools call. Local serve uses the
// in-process store. stdio mcp is an HTTP client of the daemon.
type Backend interface {
	Ask(req retrieve.Request) (retrieve.Response, error)
	Remember(rec claim.Record) (write.CatchUpResult, error)
	Get(id string) (store.RecordView, bool, error)
}

type Local struct {
	Store *store.Store
}

func (l Local) Ask(req retrieve.Request) (retrieve.Response, error) {
	return retrieve.Ask(l.Store, req)
}

func (l Local) Remember(rec claim.Record) (write.CatchUpResult, error) {
	return write.Remember(l.Store, rec)
}

func (l Local) Get(id string) (store.RecordView, bool, error) {
	rec, ok := l.Store.View(id)
	if ok {
		l.Store.RecordDwell(rec.ProjectKey, "", id)
	}
	return rec, ok, nil
}

type HTTP struct {
	BaseURL string
	Token   string
	Client  *http.Client
}

func (h HTTP) client() *http.Client {
	if h.Client != nil {
		return h.Client
	}
	return http.DefaultClient
}

func (h HTTP) Ask(req retrieve.Request) (retrieve.Response, error) {
	var out retrieve.Response
	if err := h.roundTrip(http.MethodPost, "/v1/ask", req, &out); err != nil {
		return retrieve.Response{}, err
	}
	return out, nil
}

func (h HTTP) Remember(rec claim.Record) (write.CatchUpResult, error) {
	var out write.CatchUpResult
	body := map[string]any{
		"type": rec.Type, "text": rec.Text, "project": rec.ProjectKey,
		"project_key": rec.ProjectKey, "workspace_root": rec.WorkspaceRoot,
		"paths": rec.Paths, "why": rec.Why,
	}
	if err := h.roundTrip(http.MethodPost, "/v1/remember", body, &out); err != nil {
		return write.CatchUpResult{}, err
	}
	return out, nil
}

func (h HTTP) Get(id string) (store.RecordView, bool, error) {
	var rec store.RecordView
	err := h.roundTrip(http.MethodGet, "/v1/records/"+id, nil, &rec)
	if err != nil {
		if strings.Contains(err.Error(), "404") || strings.Contains(err.Error(), "not found") {
			return store.RecordView{}, false, nil
		}
		return store.RecordView{}, false, err
	}
	if rec.ID == "" {
		return store.RecordView{}, false, nil
	}
	return rec, true, nil
}

func (h HTTP) roundTrip(method, path string, body any, dest any) error {
	base := strings.TrimRight(h.BaseURL, "/")
	if base == "" {
		base = "http://127.0.0.1:7432"
	}
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, base+path, rdr)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if h.Token != "" {
		req.Header.Set("Authorization", "Bearer "+h.Token)
	}
	res, err := h.client().Do(req)
	if err != nil {
		return fmt.Errorf("lossless daemon not reachable at %s (%v). Start it with: lossless serve", base, err)
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(res.Body)
	if err != nil {
		return err
	}
	if res.StatusCode >= 400 {
		var e struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(raw, &e)
		msg := e.Error
		if msg == "" {
			msg = strings.TrimSpace(string(raw))
		}
		if msg == "" {
			msg = res.Status
		}
		return fmt.Errorf("HTTP %d: %s", res.StatusCode, msg)
	}
	if dest == nil || len(raw) == 0 {
		return nil
	}
	return json.Unmarshal(raw, dest)
}
