package eval

import (
	"os"
	"path/filepath"
	"testing"

	"lossless/internal/bench"
)

func benchRoot(t *testing.T) string {
	t.Helper()
	root := filepath.Join("..", "testdata", "bench")
	if _, err := os.Stat(filepath.Join(root, "cases")); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestSimBenchmarkSuite(t *testing.T) {
	root := benchRoot(t)
	rep, err := bench.RunDir(root, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Log("\n" + bench.FormatReport(rep))
	if rep.CaseTotal < 8 {
		t.Fatalf("too few cases: %d", rep.CaseTotal)
	}
	for _, c := range rep.Cases {
		if !c.WriteOK {
			t.Errorf("%s write: %v", c.ID, c.WriteErrors)
		}
		for _, a := range c.Asks {
			if len(a.Errors) > 0 {
				t.Errorf("%s/%s: %v\npacket=%+v", c.ID, a.Name, a.Errors, a.Packet.Context)
			}
		}
	}
}

func TestFormatReportSmoke(t *testing.T) {
	s := bench.FormatReport(bench.Report{
		Cases: []bench.CaseScore{{
			ID: "x", WriteOK: true, AskPass: 1, AskTotal: 1, Recall: 1,
			Asks: []bench.AskScore{{Name: "a", Recall: 1}},
		}},
		CasePass: 1, CaseTotal: 1, AskPass: 1, AskTotal: 1, MeanRecall: 1,
	})
	if s == "" {
		t.Fatal("empty report")
	}
}
