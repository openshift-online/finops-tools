// cost_test.go tests JSON and CSV cost output formatting.
package output

import (
	"bytes"
	"encoding/json"
	"fmt"
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

func TestWriteCostResultCSVWithAccountBreakdownUsesRawID(t *testing.T) {
	r := fixtureResult()
	r.GroupBy = cost.GroupByAccount
	r.Breakdown = []cost.CostBreakdownItem{{
		Account:     "111111111111",
		AccountName: "Quay Production",
		Amount:      10,
	}}
	var buf bytes.Buffer
	if err := WriteCostResult(&buf, FormatCSV, r); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("lines:\n%s", buf.String())
	}
	if !strings.Contains(lines[0], "linked_account_id") {
		t.Errorf("header = %q", lines[0])
	}
	if !strings.Contains(lines[1], "111111111111") {
		t.Errorf("row missing raw account id: %q", lines[1])
	}
	if strings.Contains(lines[1], "Quay Production") {
		t.Errorf("CSV should use raw account id, not DisplayLabel: %q", lines[1])
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

func manyAccountResult(n int) cost.CostResult {
	r := fixtureResult()
	r.Linked = true
	names := make([]string, n)
	ids := make([]string, n)
	for i := 0; i < n; i++ {
		names[i] = fmt.Sprintf("Account %02d", i+1)
		ids[i] = fmt.Sprintf("%012d", i+1)
	}
	r.AccountName = strings.Join(names, ", ")
	r.AccountID = strings.Join(ids, ", ")
	return r
}

func TestWriteCostResultPrettyPrintAccountListAtLimit(t *testing.T) {
	r := manyAccountResult(maxPrettyAccountList)
	var buf bytes.Buffer
	if err := WriteCostResult(&buf, FormatPrettyPrint, r); err != nil {
		t.Fatal(err)
	}
	out := stripANSI(buf.String())
	last := fmt.Sprintf("Account %02d (%012d)", maxPrettyAccountList, maxPrettyAccountList)
	if !strings.Contains(out, "Account 01 (000000000001)") || !strings.Contains(out, last) {
		t.Errorf("missing accounts at the list limit:\n%s", out)
	}
	if strings.Contains(out, " + ") {
		t.Errorf("list at the limit should not be truncated:\n%s", out)
	}
}

func TestWriteCostResultPrettyPrintAccountListTruncatesOneExtra(t *testing.T) {
	r := manyAccountResult(maxPrettyAccountList + 1)
	var buf bytes.Buffer
	if err := WriteCostResult(&buf, FormatPrettyPrint, r); err != nil {
		t.Fatal(err)
	}
	out := stripANSI(buf.String())
	last := fmt.Sprintf("Account %02d (%012d)", maxPrettyAccountList, maxPrettyAccountList)
	omitted := fmt.Sprintf("Account %02d", maxPrettyAccountList+1)
	if !strings.Contains(out, "Account 01 (000000000001)") || !strings.Contains(out, last) {
		t.Errorf("missing first accounts:\n%s", out)
	}
	if strings.Contains(out, omitted) {
		t.Errorf("account past the limit should be omitted:\n%s", out)
	}
	if !strings.Contains(out, "+ 1 account") || strings.Contains(out, "+ 1 accounts") {
		t.Errorf("missing singular remainder:\n%s", out)
	}
}

func TestWriteCostResultPrettyPrintAccountListTruncatesMany(t *testing.T) {
	r := manyAccountResult(maxPrettyAccountList + 5)
	var buf bytes.Buffer
	if err := WriteCostResult(&buf, FormatPrettyPrint, r); err != nil {
		t.Fatal(err)
	}
	out := stripANSI(buf.String())
	last := fmt.Sprintf("Account %02d (%012d)", maxPrettyAccountList, maxPrettyAccountList)
	omitted := fmt.Sprintf("Account %02d", maxPrettyAccountList+1)
	if !strings.Contains(out, last) || strings.Contains(out, omitted) {
		t.Errorf("expected first %d accounts only:\n%s", maxPrettyAccountList, out)
	}
	if !strings.Contains(out, "+ 5 accounts") {
		t.Errorf("missing remainder suffix:\n%s", out)
	}
}

func TestWriteCostResultJSONKeepsFullAccountList(t *testing.T) {
	r := manyAccountResult(maxPrettyAccountList + 5)
	var buf bytes.Buffer
	if err := WriteCostResult(&buf, FormatJSON, r); err != nil {
		t.Fatal(err)
	}
	var decoded cost.CostResult
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.AccountName != r.AccountName || decoded.AccountID != r.AccountID {
		t.Fatalf("json account list = %q / %q", decoded.AccountName, decoded.AccountID)
	}
	lastName := fmt.Sprintf("Account %02d", maxPrettyAccountList+5)
	lastID := fmt.Sprintf("%012d", maxPrettyAccountList+5)
	if !strings.Contains(decoded.AccountName, lastName) || !strings.Contains(decoded.AccountID, lastID) {
		t.Fatalf("json dropped trailing accounts: %q / %q", decoded.AccountName, decoded.AccountID)
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
