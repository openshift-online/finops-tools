// cost_pretty_test.go tests pretty-print tables, bars, and ANSI stripping.
package output

import (
	"bytes"
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

func TestWritePrettyBreakdownColoredHeadersKeepSGR(t *testing.T) {
	tag := "prod"
	r := fixtureResult()
	r.GroupBy = cost.GroupByAccountTag
	r.GroupByTagKey = "environment"
	r.Amount = 100
	r.Breakdown = []cost.CostBreakdownItem{
		{TagValue: &tag, Amount: 100},
	}

	var buf bytes.Buffer
	s := styler{enabled: true}
	if err := writePrettyBreakdown(&buf, s, r); err != nil {
		t.Fatal(err)
	}
	raw := buf.String()
	if strings.Contains(raw, "\033[1M") || strings.Contains(raw, "\033[0M") {
		t.Fatalf("AutoFormatHeaders mangled SGR into Delete Line CSI:\n%q", raw)
	}
	for _, want := range []string{
		"\033[1mTAG VALUE\033[0m",
		"\033[1mAMOUNT\033[0m",
		"\033[1mSHARE\033[0m",
		"\033[1mRELATIVE COST\033[0m",
	} {
		if !strings.Contains(raw, want) {
			t.Fatalf("expected bold header %q, got:\n%q", want, raw)
		}
	}
	out := stripANSI(raw)
	if !strings.Contains(out, "prod") {
		t.Fatalf("rows missing:\n%s", out)
	}
}
