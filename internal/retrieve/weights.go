package retrieve

// Named caps and fusion weights. A weight change that breaks a required
// eval case is a failed change. See docs/retrieval.md.

const (
	CandidateCap    = 200
	FTSCap          = 80
	PathPerCap      = 40
	PathTotalCap    = 80
	SymbolPerCap    = 40
	SymbolTotalCap  = 80
	FailedCap       = 40
	DecisionCap     = 40
	VectorCap       = 80
	ColdPriorityCap = 30
	ColdPathCap     = 40
	ColdStateCap    = 10
	PackCap         = 5
	StaleStatCap    = 30
	DefaultLimit    = 1200
	HalfLifeDays    = 14
	DiversityJac    = 0.8
	FailedLookback  = 180
	VectorGate      = 0.55

	WFailedOverlap  = 4.0
	WShippedOverlap = 2.5
	WHotType        = 1.5
	WHotPath        = 1.2
	WHotSymbol      = 1.0
	WHotBM25        = 0.9
	WHotVector      = 0.9
	WHotRecency     = 0.8
	WStale          = 0.7

	WColdType    = 2.0
	WColdPath    = 1.2
	WColdRecency = 1.0
)

var typeRank = map[string]int{
	"failed":     5,
	"decision":   4,
	"constraint": 3,
	"state":      2,
	"thread":     1,
	"excerpt":    0,
}
