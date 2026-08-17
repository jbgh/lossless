package retrieve

// Query-conditional mixes. Failed/shipped overlap weights stay sacred
// (WFailedOverlap, WShippedOverlap). These only retune structure vs "sounds like."

type Profile int

const (
	ProfileHead Profile = iota
	ProfilePath
	ProfileIdent
	ProfileProse
)

func (p Profile) String() string {
	switch p {
	case ProfileHead:
		return "head"
	case ProfilePath:
		return "path"
	case ProfileIdent:
		return "ident"
	case ProfileProse:
		return "prose"
	default:
		return "unknown"
	}
}

type mix struct {
	typeW, path, symbol, bm25, vector, recency, agree float64
}

func mixFor(p Profile) mix {
	switch p {
	case ProfilePath:
		return mix{typeW: 1.5, path: 1.8, symbol: 0.8, bm25: 0.4, vector: 0.4, recency: 0.2, agree: WAgree}
	case ProfileIdent:
		return mix{typeW: 1.5, path: 0.8, symbol: 1.6, bm25: 0.5, vector: 0.9, recency: 0.2, agree: WAgree}
	case ProfileProse:
		return mix{typeW: 1.2, path: 0.6, symbol: 0.6, bm25: 1.3, vector: 1.3, recency: 0.2, agree: WAgree}
	default: // HEAD
		return mix{typeW: WColdType, path: WColdPath, recency: WColdRecency}
	}
}

func selectProfile(q query) Profile {
	if q.Head {
		return ProfileHead
	}
	if len(q.PathKeys) > 0 {
		return ProfilePath
	}
	if hasStrongIdent(q) {
		return ProfileIdent
	}
	return ProfileProse
}

func hasStrongIdent(q query) bool {
	for _, s := range q.Symbols {
		if identStop[s] {
			continue
		}
		if identLower(s) || containsIdentSep(s) {
			return true
		}
	}
	return false
}

func containsIdentSep(s string) bool {
	for _, r := range s {
		if r == '_' || r == '-' {
			return true
		}
	}
	return false
}

// English leftovers that tokenize as identifiers. Not code.
var identStop = map[string]bool{
	"the": true, "and": true, "for": true, "not": true, "why": true, "how": true,
	"what": true, "which": true, "this": true, "that": true, "with": true, "from": true,
	"into": true, "then": true, "than": true, "use": true, "using": true, "add": true,
	"pick": true, "know": true, "already": true, "about": true, "should": true,
	"would": true, "could": true, "library": true, "choice": true, "thing": true,
	"idea": true, "work": true, "working": true, "limit": true, "limiting": true,
	"rate": true, "want": true, "need": true, "make": true, "keep": true,
	"looking": true, "were": true, "here": true, "there": true, "last": true,
	"next": true, "again": true, "still": true, "back": true, "stuff": true,
	"going": true, "gone": true, "continue": true, "ok": true,
}
