package write

import (
	"database/sql"
	"encoding/json"
	"fmt"

	_ "modernc.org/sqlite"
)

// ReadOpenCodeSession dumps one session from OpenCode's SQLite store
// (message.data + part.data) into generic {role, content} objects.
func ReadOpenCodeSession(dbPath, sessionID string) (directory string, msgs []map[string]any, err error) {
	db, err := sql.Open("sqlite", "file:"+dbPath+"?mode=ro")
	if err != nil {
		return "", nil, err
	}
	defer db.Close()
	err = db.QueryRow(`SELECT COALESCE(directory, '') FROM session WHERE id = ?`, sessionID).Scan(&directory)
	if err != nil {
		return "", nil, fmt.Errorf("opencode session %s: %w", sessionID, err)
	}
	rows, err := db.Query(`SELECT id, data FROM message WHERE session_id = ? ORDER BY time_created, id`, sessionID)
	if err != nil {
		return directory, nil, err
	}
	defer rows.Close()
	type msgRow struct {
		id   string
		data string
	}
	var list []msgRow
	for rows.Next() {
		var r msgRow
		if err := rows.Scan(&r.id, &r.data); err != nil {
			return directory, nil, err
		}
		list = append(list, r)
	}
	if err := rows.Err(); err != nil {
		return directory, nil, err
	}
	for _, r := range list {
		var data map[string]any
		_ = json.Unmarshal([]byte(r.data), &data)
		role, _ := data["role"].(string)
		if role == "" {
			role = "other"
		}
		parts, err := loadOpenCodeParts(db, r.id)
		if err != nil {
			return directory, nil, err
		}
		content := parts
		if len(content) == 0 {
			if t, ok := data["content"].(string); ok && t != "" {
				content = []any{map[string]any{"type": "text", "text": t}}
			}
		}
		msgs = append(msgs, map[string]any{
			"type":    "message",
			"role":    role,
			"content": content,
		})
	}
	return directory, msgs, nil
}

func loadOpenCodeParts(db *sql.DB, messageID string) ([]any, error) {
	rows, err := db.Query(`SELECT data FROM part WHERE message_id = ? ORDER BY time_created, id`, messageID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []any
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		var p map[string]any
		if json.Unmarshal([]byte(raw), &p) != nil {
			continue
		}
		typ, _ := p["type"].(string)
		switch typ {
		case "text":
			if t, _ := p["text"].(string); t != "" {
				out = append(out, map[string]any{"type": "text", "text": t})
			}
		case "tool", "tool-result", "tool_result":
			if t := flatten(p["output"]); t != "" {
				out = append(out, map[string]any{"type": "text", "text": t})
			} else if t, _ := p["text"].(string); t != "" {
				out = append(out, map[string]any{"type": "text", "text": t})
			}
		case "reasoning", "step-start", "step-finish":
			continue
		default:
			if t, _ := p["text"].(string); t != "" {
				out = append(out, map[string]any{"type": "text", "text": t})
			}
		}
	}
	return out, rows.Err()
}
