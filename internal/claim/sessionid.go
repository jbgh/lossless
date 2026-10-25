package claim

import "strings"

// CleanSessionID drops sentinels children send instead of omitting the field.
// "default" must not exact-locate or bind a fake action tape id.
func CleanSessionID(s string) string {
	s = strings.TrimSpace(s)
	switch strings.ToLower(s) {
	case "", "default", "none", "null", "undefined":
		return ""
	}
	return s
}
