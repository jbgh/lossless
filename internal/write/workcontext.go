package write

import (
	"io"
	"os"
	"strings"
	"unicode/utf8"

	"lossless/internal/claim"
)

const (
	compactTailBytes = 256 << 10
	compactGoalRunes = 280
	compactPathCap   = 12
	compactMsgCap    = 8
)

// CompactWorkContext reads the tail of owned raw (not the live harness
// file) and returns the last human user line plus paths on the last few
// turns. ParseJSONL already normalized the line. Skips own ask payloads.
// Not a search string.
func CompactWorkContext(jsonl string) (goal string, paths []string) {
	if jsonl == "" {
		return "", nil
	}
	tail, base := readTail(jsonl, compactTailBytes)
	if tail == "" {
		return "", nil
	}
	msgs, _ := ParseJSONL(tail, base)
	n := 0
	for i := len(msgs) - 1; i >= 0; i-- {
		m := msgs[i]
		if m.Skip {
			continue
		}
		if goal == "" && m.Role == "user" {
			goal = oneLine(m.Text, compactGoalRunes)
		}
		if n < compactMsgCap {
			paths = append(paths, findPaths(m.Text)...)
			n++
		}
		if goal != "" && n >= compactMsgCap {
			break
		}
	}
	paths = claim.Uniq(paths)
	if len(paths) > compactPathCap {
		paths = paths[:compactPathCap]
	}
	return goal, paths
}

func readTail(path string, max int64) (data string, base int64) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil || !fi.Mode().IsRegular() {
		return "", 0
	}
	size := fi.Size()
	start := int64(0)
	if size > max {
		start = size - max
	}
	if _, err := f.Seek(start, io.SeekStart); err != nil {
		return "", 0
	}
	b, err := io.ReadAll(io.LimitReader(f, max+1))
	if err != nil {
		return "", 0
	}
	if start > 0 {
		i := 0
		for i < len(b) && b[i] != '\n' {
			i++
		}
		if i < len(b) {
			b = b[i+1:]
			start += int64(i + 1)
		}
	}
	return string(b), start
}

func oneLine(s string, maxRunes int) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if i := strings.IndexAny(s, "\n\r"); i >= 0 {
		s = strings.TrimSpace(s[:i])
	}
	if maxRunes > 0 && utf8.RuneCountInString(s) > maxRunes {
		r := []rune(s)
		s = string(r[:maxRunes])
	}
	return s
}
