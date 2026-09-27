package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"

	"lossless/internal/claim"
	"lossless/internal/write"
)

// supersedeResult is the printed shape. A reason is echoed as a decision
// record's id so a caller can get_record it and read why the constraint was
// retired.
type supersedeResult struct {
	ID         string `json:"id"`
	Status     string `json:"status"`
	Reason     string `json:"reason,omitempty"`
	ReasonID   string `json:"reason_record,omitempty"`
	ReasonKept bool   `json:"reason_recorded"`
}

// runSupersede retires a record: status becomes superseded and the record
// stops packing and warning. Delete never — the row, its text, and its export
// all survive; `get_record <id>` still returns it.
//
//	lossless supersede <id> [reason]
//
// records has no reason column, so the reason is stored as a linked decision
// record whose text names the superseded id, and whose Supersedes field points
// at it. It is written through write.Remember, so it is redacted and lands on
// the manual tape and in the export like any remembered record, and it needs
// no schema change.
//
// Works offline against the store, like inspect: it is a local edit to a local
// index, not a daemon call.
func runSupersede(args []string) int {
	fs := flag.NewFlagSet("supersede", flag.ContinueOnError)
	home := homeFlag(fs)
	asJSON := fs.Bool("json", false, "print JSON")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	rest := fs.Args()
	if len(rest) == 0 {
		fmt.Fprintln(os.Stderr, "usage: lossless supersede <id> [reason]")
		return 2
	}
	id := rest[0]
	reason := strings.TrimSpace(strings.Join(rest[1:], " "))
	if write.HomeIsRemote() {
		fmt.Fprintln(os.Stderr, "supersede edits the local store; unset LOSSLESS_URL or run it on the machine holding ~/.lossless")
		return 1
	}
	st, err := openStore(*home)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer st.Close()
	rec, ok := st.Get(id)
	if !ok {
		fmt.Fprintf(os.Stderr, "no such record: %s\n", id)
		return 1
	}
	out := supersedeResult{ID: id, Status: rec.Status, Reason: reason}
	if rec.Status == "superseded" {
		// Nothing to retire, and a second identical reason would dedupe
		// onto the first and rewrite its link.
		fmt.Fprintf(os.Stderr, "%s is already superseded; nothing changed\n", id)
		return printSupersede(*asJSON, out)
	}
	var dr claim.Record
	if reason != "" {
		// The linked decision record, written through Remember like any
		// other manual record: redacted, on the manual tape, with an
		// excerpt. Supersedes ties it to the retired row; the text names
		// the id so a plain text search finds the pair. The id is assigned
		// here so it can be reported.
		reasonText := fmt.Sprintf("Superseded %s: %s", id, reason)
		dr = claim.Record{
			ID:         claim.NewID(),
			Type:       "decision",
			Text:       reasonText,
			ProjectKey: rec.ProjectKey,
			Harness:    rec.Harness,
			SessionID:  "manual",
			Source:     "supersede",
			Supersedes: id,
			Status:     "active",
			Why:        fmt.Sprintf("retires %s", id),
			Paths:      rec.Paths,
		}
		// Checked with Remember's own probe before anything changes, so a
		// reason Remember would refuse does not leave the record retired
		// without it.
		if write.RememberRefused(dr) {
			fmt.Fprintln(os.Stderr, "the reason looks like it contains a secret; redact it and retry (nothing changed)")
			return 1
		}
	}
	if err := st.Supersede(id); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	out.Status = "superseded"
	if reason != "" {
		if _, err := write.Remember(st, dr); err != nil {
			// The record is retired either way; the non-zero exit says the
			// reason was not kept, so a script does not assume it was.
			fmt.Fprintln(os.Stderr, "superseded, but the reason record failed:", err)
			_ = printSupersede(*asJSON, out)
			return 1
		}
		out.ReasonID = dr.ID
		out.ReasonKept = true
	}
	return printSupersede(*asJSON, out)
}

func printSupersede(asJSON bool, out supersedeResult) int {
	if asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return encErr(enc.Encode(out))
	}
	fmt.Printf("superseded %s (row and text kept)\n", out.ID)
	if out.ReasonKept {
		fmt.Printf("  reason recorded as decision %s\n", out.ReasonID)
	}
	return 0
}
