package write

import (
	"strings"

	"lossless/internal/store"
)

const (
	excerptMin     = 800
	excerptTarget  = 1200
	excerptOverlap = 200
	toolClipAt     = 2000
	toolHead       = 400
	toolTail       = 400
)

func clipExcerptBody(text string) string {
	if len(text) <= toolClipAt {
		return text
	}
	return text[:toolHead] + "\n…\n" + text[len(text)-toolTail:]
}

// ChunkExcerpts windows new messages into monthly excerpt rows.
func ChunkExcerpts(session, project string, msgs []Message) []store.Excerpt {
	type piece struct {
		start, end int64
		text       string
	}
	var pieces []piece
	for _, m := range msgs {
		body := clipExcerptBody(m.Text)
		if strings.TrimSpace(body) == "" {
			continue
		}
		role := m.Role
		if role == "" {
			role = "other"
		}
		text := role + ": " + body + "\n"
		end := m.Offset + int64(len(m.Text))
		if end <= m.Offset {
			end = m.Offset + 1
		}
		pieces = append(pieces, piece{start: m.Offset, end: end, text: text})
	}
	if len(pieces) == 0 {
		return nil
	}

	var out []store.Excerpt
	i := 0
	for i < len(pieces) {
		var b strings.Builder
		start := pieces[i].start
		end := pieces[i].end
		j := i
		for j < len(pieces) {
			next := b.Len() + len(pieces[j].text)
			if b.Len() >= excerptMin && next > excerptTarget {
				break
			}
			b.WriteString(pieces[j].text)
			end = pieces[j].end
			j++
			if b.Len() >= excerptTarget {
				break
			}
		}
		if b.Len() == 0 {
			break
		}
		out = append(out, store.Excerpt{
			ID:          store.ExcerptID(session, start, end),
			SessionID:   session,
			ProjectKey:  project,
			StartOffset: start,
			EndOffset:   end,
			Text:        b.String(),
		})
		if j >= len(pieces) {
			break
		}
		startIdx := i
		i = j
		if excerptOverlap > 0 && j-startIdx > 1 {
			i = j - 1
		}
		if i <= startIdx {
			i = startIdx + 1
		}
	}
	return out
}
