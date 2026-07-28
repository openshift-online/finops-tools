// cost_test.go tests JSON and CSV cost output formatting.
package output

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/openshift-online/finops-tools/core/cost"
)

func fixtureResult() cost.CostResult {
	return cost.CostResult{
		Provider:    cost.ProviderAWS,
		AccountName: "RH Control Production",
		AccountID:   "123456789012",
		Metric:      cost.MetricNetAmortized,
		StartDate:   "2026-04-25",
		EndDate:     "2026-05-24",
		Amount:      12345678.90,
		Currency:    "USD",
	}
}

func TestWriteCostResultPrettyPrint(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteCostResult(&buf, FormatPrettyPrint, fixtureResult()); err != nil {
		t.Fatal(err)
	}
	out := stripANSI(buf.String())
	for _, want := range []string{
		"Net amortized cost (30 days)", // 2026-04-25 through 2026-05-24 inclusive
		"USD 12,345,678.90",
		"Payer:",
		"RH Control Production (123456789012)",
		"2026-04-25 – 2026-05-24",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

func TestWriteCostResultJSON(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteCostResult(&buf, FormatJSON, fixtureResult()); err != nil {
		t.Fatal(err)
	}
	var decoded cost.CostResult
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Amount != fixtureResult().Amount {
		t.Errorf("amount = %v", decoded.Amount)
	}
}

func TestWriteCostResultCSV(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteCostResult(&buf, FormatCSV, fixtureResult()); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("lines = %d, want 2:\n%s", len(lines), buf.String())
	}
	if !strings.HasPrefix(lines[0], "provider,account_name") {
		t.Errorf("header = %q", lines[0])
	}
	if !strings.Contains(lines[1], "RH Control Production") {
		t.Errorf("row = %q", lines[1])
	}
}

func TestWriteCostResultPrettyPrintWithBreakdown(t *testing.T) {
	r := fixtureResult()
	r.GroupBy = cost.GroupByService
	r.Breakdown = []cost.CostBreakdownItem{
		{Service: "Amazon EC2", Amount: 100},
		{Service: "Amazon S3", Amount: 10},
	}
	var buf bytes.Buffer
	if err := WriteCostResult(&buf, FormatPrettyPrint, r); err != nil {
		t.Fatal(err)
	}
	out := stripANSI(buf.String())
	if !strings.Contains(out, "Cost by service") {
		t.Errorf("missing breakdown header:\n%s", out)
	}
	if !strings.Contains(out, "Amazon EC2") || !strings.Contains(out, "100.00") {
		t.Errorf("missing EC2 line:\n%s", out)
	}
}

func TestWriteCostResultCSVWithBreakdown(t *testing.T) {
	r := fixtureResult()
	r.Breakdown = []cost.CostBreakdownItem{{Service: "Amazon S3", Amount: 10}}
	var buf bytes.Buffer
	if err := WriteCostResult(&buf, FormatCSV, r); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("lines:\n%s", buf.String())
	}
	if !strings.Contains(lines[0], "service") {
		t.Errorf("header = %q", lines[0])
	}
	if !strings.Contains(lines[1], "Amazon S3") {
		t.Errorf("row = %q", lines[1])
	}
}

func TestWriteCostResultPrettyPrintLinkedAccount(t *testing.T) {
	r := fixtureResult()
	r.Linked = true
	r.AccountName = "Quay Production"
	r.AccountID = "111111111111"
	var buf bytes.Buffer
	if err := WriteCostResult(&buf, FormatPrettyPrint, r); err != nil {
		t.Fatal(err)
	}
	out := stripANSI(buf.String())
	if strings.Contains(out, "Payer:") {
		t.Errorf("linked account should not use Payer label:\n%s", out)
	}
	if !strings.Contains(out, "Account:") || !strings.Contains(out, "Quay Production (111111111111)") {
		t.Errorf("missing linked account label:\n%s", out)
	}
}

func TestWriteCostResultPrettyPrintMergedPayers(t *testing.T) {
	r := fixtureResult()
	r.AccountName = "RH Control Production, OSD Staging"
	r.AccountID = "111, 222"
	r.Amount = 15000
	var buf bytes.Buffer
	if err := WriteCostResult(&buf, FormatPrettyPrint, r); err != nil {
		t.Fatal(err)
	}
	out := stripANSI(buf.String())
	if !strings.Contains(out, "Payers:") || !strings.Contains(out, "RH Control Production (111)") {
		t.Errorf("missing merged payers:\n%s", out)
	}
	if strings.Contains(out, "Combined total") {
		t.Error("should not show separate combined line when already merged")
	}
}

func TestParseFormat(t *testing.T) {
	f, err := ParseFormat("JSON")
	if err != nil || f != FormatJSON {
		t.Fatalf("got %v %v", f, err)
	}
	_, err = ParseFormat("yaml")
	if err == nil {
		t.Fatal("expected error")
	}
}
