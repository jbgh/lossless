package mcpserver

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"lossless/internal/version"
)

const ProtocolVersion = "2025-03-26"

const maxRPCBytes = 2 << 20

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type Server struct {
	Backend Backend
	Name    string
	Version string
}

func New(b Backend) *Server {
	return &Server{Backend: b, Name: "lossless", Version: version.Version}
}

func (s *Server) Handle(raw []byte) []byte {
	var req rpcRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		return mustRPC(rpcResponse{
			JSONRPC: "2.0",
			Error:   &rpcError{Code: -32700, Message: "parse error"},
		})
	}
	if req.JSONRPC == "" {
		req.JSONRPC = "2.0"
	}
	if req.Method == "" {
		return mustRPC(rpcResponse{JSONRPC: "2.0", ID: req.ID, Error: &rpcError{Code: -32600, Message: "invalid request"}})
	}
	// notifications have no id
	if isNotification(req.ID) {
		if req.Method == "notifications/initialized" || req.Method == "initialized" {
			return nil
		}
		return nil
	}
	result, err := s.dispatch(req)
	if err != nil {
		code := -32603
		if strings.Contains(err.Error(), "unknown method") {
			code = -32601
		}
		if strings.Contains(err.Error(), "unknown tool") || strings.Contains(err.Error(), "invalid") {
			code = -32602
		}
		return mustRPC(rpcResponse{JSONRPC: "2.0", ID: req.ID, Error: &rpcError{Code: code, Message: err.Error()}})
	}
	return mustRPC(rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: result})
}

func (s *Server) dispatch(req rpcRequest) (any, error) {
	switch req.Method {
	case "initialize":
		return s.initialize(req.Params)
	case "ping":
		return map[string]any{}, nil
	case "tools/list":
		return map[string]any{"tools": toolDefs()}, nil
	case "tools/call":
		return s.callTool(req.Params)
	case "notifications/initialized", "initialized":
		return map[string]any{}, nil
	default:
		return nil, fmt.Errorf("unknown method: %s", req.Method)
	}
}

func (s *Server) initialize(params json.RawMessage) (any, error) {
	ver := ProtocolVersion
	var p struct {
		ProtocolVersion string `json:"protocolVersion"`
	}
	if len(params) > 0 {
		_ = json.Unmarshal(params, &p)
		if p.ProtocolVersion != "" {
			ver = p.ProtocolVersion
		}
	}
	name, infoVer := s.Name, s.Version
	if name == "" {
		name = "lossless"
	}
	if infoVer == "" {
		infoVer = version.Version
	}
	return map[string]any{
		"protocolVersion": ver,
		"capabilities":    map[string]any{"tools": map[string]any{}},
		"serverInfo":      map[string]any{"name": name, "version": infoVer},
	}, nil
}

func isNotification(id json.RawMessage) bool {
	if len(id) == 0 || string(id) == "null" {
		return true
	}
	return false
}

func mustRPC(r rpcResponse) []byte {
	b, err := json.Marshal(r)
	if err != nil {
		return []byte(`{"jsonrpc":"2.0","error":{"code":-32603,"message":"internal"}}`)
	}
	return b
}

// HTTPHandler serves streamable-HTTP-compatible JSON-RPC at /mcp.
func (s *Server) HTTPHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		body, err := io.ReadAll(io.LimitReader(r.Body, maxRPCBytes))
		if err != nil || len(bytes.TrimSpace(body)) == 0 {
			http.Error(w, "invalid JSON", http.StatusBadRequest)
			return
		}
		out := s.Handle(body)
		if out == nil {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		if strings.Contains(r.Header.Get("Accept"), "text/event-stream") {
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			_, _ = fmt.Fprintf(w, "data: %s\n\n", out)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(out)
		_, _ = w.Write([]byte("\n"))
	})
}

// ServeStdio reads newline-delimited (or Content-Length framed) JSON-RPC from in.
func (s *Server) ServeStdio(in io.Reader, out io.Writer) error {
	r := bufio.NewReader(in)
	for {
		msg, err := readFrame(r)
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		if len(bytes.TrimSpace(msg)) == 0 {
			continue
		}
		resp := s.Handle(msg)
		if resp == nil {
			continue
		}
		if _, err := out.Write(append(resp, '\n')); err != nil {
			return err
		}
	}
}

func readFrame(r *bufio.Reader) ([]byte, error) {
	// Detect Content-Length framing (LSP-style) vs newline-delimited JSON.
	prefix, err := r.Peek(15)
	if err != nil && err != io.EOF && len(prefix) == 0 {
		return nil, err
	}
	if bytes.HasPrefix(bytes.ToLower(prefix), []byte("content-length:")) {
		return readContentLength(r)
	}
	line, err := r.ReadBytes('\n')
	if err != nil && len(line) == 0 {
		return nil, err
	}
	return bytes.TrimRight(line, "\r\n"), nil
}

func readContentLength(r *bufio.Reader) ([]byte, error) {
	var n int
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = strings.TrimSpace(line)
		if line == "" {
			break
		}
		low := strings.ToLower(line)
		if strings.HasPrefix(low, "content-length:") {
			v := strings.TrimSpace(line[len("Content-Length:"):])
			n, err = strconv.Atoi(v)
			if err != nil {
				return nil, err
			}
		}
	}
	if n <= 0 {
		return nil, fmt.Errorf("missing Content-Length")
	}
	if n > maxRPCBytes {
		return nil, fmt.Errorf("Content-Length too large")
	}
	buf := make([]byte, n)
	if _, err := io.ReadFull(r, buf); err != nil {
		return nil, err
	}
	return buf, nil
}
