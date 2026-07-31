// cost_pretty_test.go tests pretty-print tables, bars, and ANSI stripping.
package output

import (
	"strings"
	"testing"

	"github.com/openshift-online/finops-tools/core/cost"
)

func TestRenderBarPlain(t *testing.T) {
	s := styler{enabled: false}
	got := renderBar(s, 0.5, 0)
	if strings.Contains(got, "\033") {
		t.Fatalf("expected no ANSI: %q", got)
	}
	filled := strings.Count(got, "█")
	if filled != 12 {
		t.Errorf("filled blocks = %d, want 12", filled)
	}
}

func TestRenderBarColored(t *testing.T) {
	s := styler{enabled: true}
	got := renderBar(s, 1, 0)
	if !strings.Contains(got, "\033") {
		t.Fatal("expected ANSI color codes")
	}
}

func TestWritePrettyPrintNoANSIInBuffer(t *testing.T) {
	r := fixtureResult()
	r.Breakdown = []cost.CostBreakdownItem{
		{Service: "Amazon EC2", Amount: 90},
		{Service: "Amazon S3", Amount: 10},
	}
	r.Amount = 100
	r.GroupBy = cost.GroupByService

	var buf strings.Builder
	if err := writePrettyPrint(&buf, r); err != nil {
		t.Fatal(err)
	}
	out := stripANSI(buf.String())
	if strings.Contains(out, "\033") {
		t.Fatal("buffer output should not contain escape codes")
	}
	for _, want := range []string{
		"Net amortized cost (30 days)",
		"Cost by service",
		"Amazon EC2",
		"90.00",
		"█",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}
