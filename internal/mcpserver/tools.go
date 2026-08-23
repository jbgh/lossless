package mcpserver

import (
	"encoding/json"
	"fmt"

	"lossless/internal/claim"
	"lossless/internal/retrieve"
)

func toolDefs() []map[string]any {
	return []map[string]any{
		{
			"name":        "ask",
			"description": "Required before implementing, changing behavior, or continuing after compact. Returns past failed/decisions/constraints for the current goal and files in context (≤5). Send workspace_root, goal, paths, and session_id when the harness has one. Treat warnings as blocking unless the user overrides. Do not wait for the user to mention lossless. Skip trivia.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"question":       map[string]any{"type": "string", "description": "Natural-language question. Empty + empty goal is a cold ask."},
					"project":        map[string]any{"type": "string", "description": "owner/repo. Required if workspace_root is omitted."},
					"workspace_root": map[string]any{"type": "string", "description": "Absolute git checkout of this repo (origin derives owner/repo). Used for [verify] mtimes."},
					"goal":           map[string]any{"type": "string", "description": "What the agent is about to do."},
					"paths":          map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Repo-relative files in play."},
					"session_id":     map[string]any{"type": "string", "description": "Harness session id when present. Omit only if the harness did not give one. Binds catch-up and the action tape. Do not send default."},
					"limit_tokens":   map[string]any{"type": "integer", "description": "Token budget for context. Default 1200."},
				},
			},
		},
		{
			"name":        "remember",
			"description": "Assert a durable claim now (decision, failed, constraint, state, thread). Additive; does not replace session catch-up.",
			"inputSchema": map[string]any{
				"type":     "object",
				"required": []string{"type", "text"},
				"properties": map[string]any{
					"type":           map[string]any{"type": "string", "enum": []string{"failed", "decision", "constraint", "state", "thread"}},
					"text":           map[string]any{"type": "string", "description": "The durable sentence to keep."},
					"project":        map[string]any{"type": "string"},
					"workspace_root": map[string]any{"type": "string"},
					"paths":          map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
					"why":            map[string]any{"type": "string"},
				},
			},
		},
		{
			"name":        "get_record",
			"description": "Open the tape excerpt and source for one ask hit. Required when the packed sentence is not enough to act without guessing (recap, slogan, or you would change extract/gate/behavior). Not required on every hit. Does not change ask context. Not a search.",
			"inputSchema": map[string]any{
				"type":     "object",
				"required": []string{"id"},
				"properties": map[string]any{
					"id": map[string]any{"type": "string"},
				},
			},
		},
	}
}

func (s *Server) callTool(params json.RawMessage) (any, error) {
	var p struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("invalid tools/call params")
	}
	if s.Backend == nil {
		return toolErr("no backend"), nil
	}
	switch p.Name {
	case "ask":
		return s.toolAsk(p.Arguments)
	case "remember":
		return s.toolRemember(p.Arguments)
	case "get_record":
		return s.toolGet(p.Arguments)
	default:
		return nil, fmt.Errorf("unknown tool: %s", p.Name)
	}
}

func (s *Server) toolAsk(args json.RawMessage) (any, error) {
	var req retrieve.Request
	if len(args) > 0 {
		if err := json.Unmarshal(args, &req); err != nil {
			return toolErr("invalid ask arguments"), nil
		}
	}
	out, err := s.Backend.Ask(req)
	if err != nil {
		return toolErr(err.Error()), nil
	}
	return toolOK(out), nil
}

func (s *Server) toolRemember(args json.RawMessage) (any, error) {
	var body struct {
		claim.Record
		Project string `json:"project"`
	}
	if err := json.Unmarshal(args, &body); err != nil {
		return toolErr("invalid remember arguments"), nil
	}
	rec := body.Record
	if rec.ProjectKey == "" {
		rec.ProjectKey = body.Project
	}
	out, err := s.Backend.Remember(rec)
	if err != nil {
		return toolErr(err.Error()), nil
	}
	return toolOK(out), nil
}

func (s *Server) toolGet(args json.RawMessage) (any, error) {
	var p struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(args, &p); err != nil || p.ID == "" {
		return toolErr("id required"), nil
	}
	rec, ok, err := s.Backend.Get(p.ID)
	if err != nil {
		return toolErr(err.Error()), nil
	}
	if !ok {
		return toolErr("not found"), nil
	}
	return toolOK(rec), nil
}

func toolOK(v any) map[string]any {
	b, _ := json.Marshal(v)
	return map[string]any{
		"content":           []map[string]any{{"type": "text", "text": string(b)}},
		"structuredContent": v,
		"isError":           false,
	}
}

func toolErr(msg string) map[string]any {
	return map[string]any{
		"content": []map[string]any{{"type": "text", "text": msg}},
		"isError": true,
	}
}
