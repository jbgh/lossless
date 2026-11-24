package retrieve

import (
	"testing"

	"lossless/internal/claim"
)

// v2cand is a candidate the lexical route found with raw BM25 fts (0 for
// none) that shares npath of the ask's files.
func v2cand(id, typ, source, text string, fts float64, npath int) scored {
	c := scored{rec: claim.Record{ID: id, Type: typ, Source: source, Text: text, ClaimHash: id, CreatedAt: "2026-10-01T00:00:00Z"}, npath: npath}
	if npath > 0 {
		c.path = 1
	}
	if fts != 0 {
		c.isFTS, c.ftsRaw = true, -fts
		c.ownFTS, c.ownRaw = true, -fts
	}
	return c
}

func v2ids(cs []scored) string {
	s := ""
	for _, c := range cs {
		s += c.rec.ID + " "
	}
	return s
}

// The pack is in relevance order: wording match plus a fixed bonus per
// shared file. Type and source do not enter it, so a turn-extracted
// failure that matches better outranks a remembered decision, and three
// decisions can fill the pack.
func TestPackV2RanksByRelevanceNotType(t *testing.T) {
	cand := []scored{
		v2cand("F1", "failed", "turn", "The sauna planner test failed again while checking the build.", 30, 0),
		v2cand("D1", "decision", "remember", "Shops close at dusk in the day loop.", 25, 1),
		v2cand("D2", "decision", "remember", "The day loop lasts eight minutes on every level.", 28, 0),
		v2cand("D3", "decision", "remember", "Dusk dims the street lights before the shops close.", 12, 0),
	}
	if got := v2ids(packV2(cand, 100000, nil)); got != "D1 F1 D2 D3 " {
		t.Fatalf("want D1 (25+10) F1 (30) D2 (28) D3 (12), got %s", got)
	}
}

// A shared file is a bonus, not a trump: on a long goal the wording match
// of a record on another file can be worth more than the file alone.
func TestPackV2SharedFileDoesNotOutrankAStrongMatch(t *testing.T) {
	cand := []scored{
		v2cand("P1", "decision", "remember", "A note that only shares the file.", 0, 1),
		v2cand("L1", "failed", "remember", "The record that says what the goal says.", 60, 0),
	}
	if got := v2ids(packV2(cand, 100000, nil)); got != "L1 P1 " {
		t.Fatalf("want the wording match first, got %s", got)
	}
}

// The cut compares wording matches with the best wording match. In a
// small store BM25 is small and one shared file outscores everything; a
// cut against the top score would drop every record that matched on words.
func TestPackV2CutIgnoresTheFileBonus(t *testing.T) {
	cand := []scored{
		v2cand("P1", "failed", "turn", "Redis token bucket failed in the middleware.", 0, 1),
		v2cand("L1", "decision", "remember", "Picked jose for the token library.", 1.5, 0),
		v2cand("W1", "state", "turn", "A much weaker wording match.", 0.2, 0),
	}
	if got := v2ids(packV2(cand, 100000, nil)); got != "P1 L1 " {
		t.Fatalf("want the file match and the wording match, not the weak one, got %s", got)
	}
}

// A record with no match of its own, on a file the first hits named, packs
// when the best match is modest and is cut when real matches are strong.
func TestPackV2HopFillsOnlyASmallPack(t *testing.T) {
	hop := v2cand("H1", "failed", "turn", "Redis token bucket failed in staging.", 0, 0)
	hop.hop = 0.5
	small := []scored{v2cand("D1", "decision", "remember", "Rate limiter lives in the middleware.", 2, 0), hop}
	if got := v2ids(packV2(small, 100000, nil)); got != "D1 H1 " {
		t.Fatalf("small store: want the hop record packed, got %s", got)
	}
	big := []scored{v2cand("D1", "decision", "remember", "Rate limiter lives in the middleware.", 40, 0), hop}
	if got := v2ids(packV2(big, 100000, nil)); got != "D1 " {
		t.Fatalf("strong match: want the hop record cut, got %s", got)
	}
}

// The trace names why v2 left a record out, in v2's own terms.
func TestPackSkipReasonV2(t *testing.T) {
	top := v2cand("T1", "constraint", "remember", "The rule this ask is about.", 10, 1)
	weak := v2cand("W1", "state", "turn", "A weak lexical hit.", 0.5, 0)
	off := v2cand("U1", "decision", "remember", "Unrelated.", 6, 0)
	off.ungrounded = true
	net := v2cand("N1", "failed", "turn", "A recent failure.", 0, 0)
	cand := []scored{top, weak, off, net}
	nets := map[string]bool{"N1": true}
	packed := packV2(cand, 100000, nets)
	floor := cutV2(cand)
	text := [][]string{claim.Tokens(top.rec.Text)}
	for i, want := range []string{"", "below-cut", "off-topic", "recent-failed-net"} {
		if i == 0 {
			continue
		}
		if got := packSkipReasonV2(cand[i], packed, text, nets, floor); got != want {
			t.Fatalf("%s: want %s, got %s", cand[i].rec.ID, want, got)
		}
	}
}

// The relevance floor and the recent-failed net still keep unrelated
// records out, and a hit far below the best is left out.
func TestPackV2LeavesOutUnrelated(t *testing.T) {
	off := v2cand("U1", "decision", "remember", "Unrelated.", 6, 0)
	off.ungrounded = true
	net := v2cand("N1", "failed", "turn", "A recent failure.", 0, 0)
	weak := v2cand("W1", "state", "turn", "A weak lexical hit.", 0.5, 0)
	top := v2cand("T1", "constraint", "remember", "The rule this ask is about.", 10, 1)
	got := packV2([]scored{off, net, weak, top}, 100000, map[string]bool{"N1": true})
	if len(got) != 1 || got[0].rec.ID != "T1" {
		t.Fatalf("want only T1, got %v", got)
	}
}

// v2 is the default; v1 is asked for by name, on the engine or in the env.
func TestRankerV2Switch(t *testing.T) {
	t.Setenv("LOSSLESS_RANKER", "")
	if !(Engine{}).RankerV2() {
		t.Fatal("v2 is the default")
	}
	t.Setenv("LOSSLESS_RANKER", "v1")
	if (Engine{}).RankerV2() {
		t.Fatal("the env should bring v1 back")
	}
	if !(Engine{Ranker: "v2"}).RankerV2() {
		t.Fatal("Engine.Ranker wins over the env")
	}
	t.Setenv("LOSSLESS_RANKER", "")
	if (Engine{Ranker: "v1"}).RankerV2() {
		t.Fatal("Engine.Ranker v1 should turn v2 off")
	}
}

// Of two near-identical records of one type, the newer is the one packed,
// also when the older scores higher because the ask names the old value.
func TestPackV2KeepsTheNewerOfTwoRestatements(t *testing.T) {
	old := v2cand("OLD", "decision", "remember", "The list endpoint in the handlers package returns pages of fifty items sorted by creation date.", 35, 1)
	old.rec.CreatedAt = "2026-06-01T00:00:00Z"
	newer := v2cand("NEW", "decision", "remember", "The list endpoint in the handlers package returns pages of hundred items sorted by creation date.", 28, 1)
	newer.rec.CreatedAt = "2026-08-01T00:00:00Z"
	other := v2cand("OTHER", "failed", "remember", "Raising the page size past two hundred timed out the mobile client.", 20, 0)
	if got := v2ids(packV2([]scored{old, newer, other}, 100000, nil)); got != "NEW OTHER " {
		t.Fatalf("want the newer restatement in the older one's place, got %s", got)
	}
	// The other way round the older one is the lower-scored twin and is
	// simply skipped.
	old.ftsRaw, old.ownRaw = -20, -20
	if got := v2ids(packV2([]scored{old, newer, other}, 100000, nil)); got != "NEW OTHER " {
		t.Fatalf("want the newer restatement only, got %s", got)
	}
}

const (
	twinOld = "The list endpoint in the handlers package returns pages of fifty items sorted by creation date."
	twinNew = "The list endpoint in the handlers package returns pages of hundred items sorted by creation date."
)

// The newer restatement takes the older one's place also when the pack
// was full before it came up, and when its own score is under the cut.
func TestPackV2NewerTwinReplacesPastTheCapAndTheCut(t *testing.T) {
	old := v2cand("OLD", "decision", "remember", twinOld, 35, 0)
	old.rec.CreatedAt = "2026-06-01T00:00:00Z"
	newer := v2cand("NEW", "decision", "remember", twinNew, 20, 0)
	newer.rec.CreatedAt = "2026-08-01T00:00:00Z"
	full := []scored{old,
		v2cand("X0", "failed", "remember", "Raising the page size past two hundred timed out the mobile client.", 30, 0),
		v2cand("X1", "constraint", "remember", "Never return more than five hundred rows from any endpoint.", 29, 0),
		v2cand("X2", "state", "remember", "Pagination cursors are opaque base64 strings since October.", 28, 0),
		v2cand("X3", "decision", "remember", "Sorting falls back to the id column when two dates tie.", 27, 0),
		newer,
	}
	if got := v2ids(packV2(full, 100000, nil)); got != "NEW X0 X1 X2 X3 " {
		t.Fatalf("full pack: want NEW in OLD's place, got %s", got)
	}
	newer.ftsRaw, newer.ownRaw = -6, -6 // under 20% of 35
	if got := v2ids(packV2([]scored{old, newer}, 100000, nil)); got != "NEW " {
		t.Fatalf("under the cut: want NEW, got %s", got)
	}
}

// A record an agent wrote down is not replaced by a turn's echo of it,
// newer or not: the echo may not warn, so the swap would lose a warning.
func TestPackV2RememberedRecordStandsOverATurnEcho(t *testing.T) {
	kept := v2cand("KEPT", "failed", "remember", twinOld, 30, 0)
	kept.rec.CreatedAt = "2026-06-01T00:00:00Z"
	echo := v2cand("ECHO", "failed", "turn", twinNew, 28, 0)
	echo.rec.CreatedAt = "2026-08-01T00:00:00Z"
	if got := v2ids(packV2([]scored{kept, echo}, 100000, nil)); got != "KEPT " {
		t.Fatalf("want the remembered record, got %s", got)
	}
	// Also when the echo scores higher and is packed first.
	echo.ftsRaw, echo.ownRaw = -40, -40
	if got := v2ids(packV2([]scored{kept, echo}, 100000, nil)); got != "KEPT " {
		t.Fatalf("echo packed first: want the remembered record, got %s", got)
	}
}

// A newer twin that would take the pack over the token limit does not
// replace the record already packed.
func TestPackV2TwinSwapKeepsTheTokenLimit(t *testing.T) {
	old := v2cand("OLD", "decision", "remember", twinOld, 35, 0)
	old.rec.CreatedAt = "2026-06-01T00:00:00Z"
	newer := v2cand("NEW", "decision", "remember", twinNew, 20, 0)
	newer.rec.CreatedAt = "2026-08-01T00:00:00Z"
	for i := 0; i < 60; i++ {
		newer.rec.Paths = append(newer.rec.Paths, "internal/handlers/list/some/long/path/file"+string(rune('a'+i%26))+".go")
	}
	limit := estimateTokens(mustJSON(toHit(old, false))) + 10
	if got := v2ids(packV2([]scored{old, newer}, limit, nil)); got != "OLD " {
		t.Fatalf("want OLD kept under the limit, got %s", got)
	}
}

// A meaning match has no BM25 of its own. It is scaled to the best
// wording match, so a close paraphrase ranks beside the records that
// share the ask's words and a distant one stays under the cut.
func TestPackV2VectorHitIsScaledToTheBestWordingMatch(t *testing.T) {
	lex := v2cand("LEX", "decision", "remember", "Rate limiter lives in the middleware.", 30, 0)
	weak := v2cand("WEAK", "state", "remember", "The limiter config is read at boot.", 10, 0)
	near := v2cand("NEAR", "failed", "remember", "Throttling requests per tenant broke the websocket handshake.", 0, 0)
	near.vector = 0.8
	far := v2cand("FAR", "failed", "remember", "The colour palette moved to tokens.", 0, 0)
	far.vector = 0.1
	if got := v2ids(packV2([]scored{lex, weak, near, far}, 100000, nil)); got != "LEX NEAR WEAK " {
		t.Fatalf("want LEX (30) NEAR (0.8×30) WEAK (10), FAR (0.1×30) cut, got %s", got)
	}
}
