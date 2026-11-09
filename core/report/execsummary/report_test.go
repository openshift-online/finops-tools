// report_test.go tests the executive summary report model, aggregation, and KPIs.
package execsummary

import (
	"testing"
	"time"

	"github.com/openshift-online/finops-tools/core/cost"
)

const (
	payerID = "123456789012"
	memberA = "111111111111"
	memberB = "222222222222"
)

func monthTime(year int, month time.Month) time.Time {
	return time.Date(year, month, 1, 0, 0, 0, 0, time.UTC)
}

func fiveMonthWindow() TimeWindow {
	// Feb–Jun 2026 (today in July 2026): includes 28-day February.
	return TimeWindow{
		Start: monthTime(2026, time.February),
		End:   monthTime(2026, time.June),
		Months: []time.Time{
			monthTime(2026, time.February),
			monthTime(2026, time.March),
			monthTime(2026, time.April),
			monthTime(2026, time.May),
			monthTime(2026, time.June),
		},
	}
}

func sampleResult() cost.MonthlyAccountServiceResult {
	return cost.MonthlyAccountServiceResult{
		PayerAccountID: payerID,
		Currency:       "USD",
		Rows: []cost.MonthlyGroupedCost{
			// February (28 days): payer 28, member A 56 → total 84 → daily avg 3
			{Month: "2026-02", AccountID: payerID, Service: "Amazon S3", Amount: 28},
			{Month: "2026-02", AccountID: memberA, Service: "Amazon EC2", Amount: 56},
			// June (30 days): payer 30, member A 60 → total 90 → daily avg 3 (flat MoM)
			{Month: "2026-06", AccountID: payerID, Service: "Amazon S3", Amount: 30},
			{Month: "2026-06", AccountID: memberA, Service: "Amazon EC2", Amount: 60},
			// March (31 days): member B only, 31 → daily avg 1
			{Month: "2026-03", AccountID: memberB, Service: "AWS Lambda", Amount: 31},
			// May (31 days): payer 31, member A 62 → total 93 → daily avg 3
			{Month: "2026-05", AccountID: payerID, Service: "Amazon S3", Amount: 31},
			{Month: "2026-05", AccountID: memberA, Service: "Amazon EC2", Amount: 62},
		},
	}
}

func TestBuildReportMonthlyTotalsAndMoM(t *testing.T) {
	rep, err := BuildReport("example-payer", sampleResult(), fiveMonthWindow(), map[string]string{
		memberA: "Member A",
	})
	if err != nil {
		t.Fatal(err)
	}

	if rep.PayerAlias != "example-payer" || rep.PayerAccountID != payerID {
		t.Errorf("payer identity = %q/%q", rep.PayerAlias, rep.PayerAccountID)
	}
	if rep.Currency != "USD" {
		t.Errorf("currency = %q", rep.Currency)
	}
	if len(rep.Months) != 5 {
		t.Fatalf("months = %d, want 5", len(rep.Months))
	}

	byMonth := map[string]MonthSummary{}
	for _, m := range rep.Months {
		byMonth[m.Month] = m
	}

	feb := byMonth["2026-02"]
	if feb.Total != 84 {
		t.Errorf("feb total = %v, want 84", feb.Total)
	}
	if feb.DailyAverage != 3 {
		t.Errorf("feb daily average = %v, want 3", feb.DailyAverage)
	}
	if feb.MoMPct != nil {
		t.Errorf("first month MoM = %v, want nil (N/A)", feb.MoMPct)
	}

	mar := byMonth["2026-03"]
	if mar.Total != 31 || mar.DailyAverage != 1 {
		t.Errorf("mar total/daily = %v/%v, want 31/1", mar.Total, mar.DailyAverage)
	}
	if mar.MoMPct == nil || *mar.MoMPct != (1.0/3.0-1)*100 {
		t.Errorf("mar MoM = %v, want normalized (1/3-1)*100", mar.MoMPct)
	}

	// Missing months count as zero totals with zero daily average.
	apr := byMonth["2026-04"]
	if apr.Total != 0 || apr.DailyAverage != 0 {
		t.Errorf("apr total/daily = %v/%v, want 0/0", apr.Total, apr.DailyAverage)
	}

	jun := byMonth["2026-06"]
	if jun.Total != 90 || jun.DailyAverage != 3 {
		t.Errorf("jun total/daily = %v/%v, want 90/3", jun.Total, jun.DailyAverage)
	}
	if jun.MoMPct == nil || *jun.MoMPct != 0 {
		t.Errorf("jun MoM = %v, want 0 (equal daily averages despite different totals)", jun.MoMPct)
	}
}

func TestBuildReportZeroPriorMonthMoMIsNA(t *testing.T) {
	rep, err := BuildReport("example-payer", sampleResult(), fiveMonthWindow(), nil)
	if err != nil {
		t.Fatal(err)
	}
	// April has zero prior-month (March total 31 > 0)... use May: prior month April is zero.
	for _, m := range rep.Months {
		if m.Month == "2026-05" {
			if m.MoMPct != nil {
				t.Errorf("MoM for month after zero-cost month = %v, want nil (N/A)", m.MoMPct)
			}
		}
	}
}

func TestBuildReportAccounts(t *testing.T) {
	rep, err := BuildReport("example-payer", sampleResult(), fiveMonthWindow(), map[string]string{
		payerID: "Payer Account",
		memberA: "Member A",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Accounts) != 3 {
		t.Fatalf("accounts = %d, want 3", len(rep.Accounts))
	}
	byID := map[string]AccountSummary{}
	for _, a := range rep.Accounts {
		byID[a.AccountID] = a
	}
	if !byID[payerID].IsPayer {
		t.Error("payer account not flagged")
	}
	if byID[memberA].IsPayer {
		t.Error("member flagged as payer")
	}
	if byID[memberA].DisplayName != "Member A" {
		t.Errorf("member A display name = %q", byID[memberA].DisplayName)
	}
	if byID[memberB].DisplayName != memberB {
		t.Errorf("unnamed account should fall back to ID, got %q", byID[memberB].DisplayName)
	}
	// Account totals across the window.
	if byID[memberA].Total != 178 {
		t.Errorf("member A total = %v, want 178", byID[memberA].Total)
	}
	if byID[payerID].Total != 89 {
		t.Errorf("payer total = %v, want 89", byID[payerID].Total)
	}
	// Service rows retained per account/month.
	if len(byID[memberA].Services) != 3 {
		t.Errorf("member A service rows = %d, want 3", len(byID[memberA].Services))
	}
}

func TestBuildReportZeroSpendAccountsTallied(t *testing.T) {
	res := sampleResult()
	// Three selected accounts (payer + two members) plus two with no rows.
	names := map[string]string{
		payerID:  "Payer",
		memberA:  "Member A",
		memberB:  "Member B",
		"333333333333": "Idle One",
		"444444444444": "Idle Two",
	}
	rep, err := BuildReport("example-payer", res, fiveMonthWindow(), names)
	if err != nil {
		t.Fatal(err)
	}
	if rep.ZeroSpendAccounts != 2 {
		t.Errorf("ZeroSpendAccounts = %d, want 2", rep.ZeroSpendAccounts)
	}
	for _, a := range rep.Accounts {
		if a.AccountID == "333333333333" || a.AccountID == "444444444444" {
			t.Errorf("zero-spend account %s should not be listed in Accounts", a.AccountID)
		}
	}
}

func TestBuildReportRowOutsideWindowFails(t *testing.T) {
	res := sampleResult()
	res.Rows = append(res.Rows, cost.MonthlyGroupedCost{Month: "2026-07", AccountID: memberA, Service: "Amazon EC2", Amount: 5})
	_, err := BuildReport("example-payer", res, fiveMonthWindow(), nil)
	if err == nil {
		t.Fatal("expected error for row outside the window")
	}
}

func TestBuildReportPayerWithNoOwnSpendStillListed(t *testing.T) {
	res := sampleResult()
	res.Rows = nil
	rep, err := BuildReport("example-payer", res, fiveMonthWindow(), nil)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, a := range rep.Accounts {
		if a.AccountID == payerID && a.IsPayer {
			found = true
		}
	}
	if !found {
		t.Error("payer with no own spend should still be listed")
	}
	if len(rep.Months) != 5 {
		t.Errorf("months = %d, want 5", len(rep.Months))
	}
}

// TestBuildReportLeapYearFebruary documents the calendar-day normalization for
// a 29-day February: 29 spend over 29 days averages 1/day like a 31-day month
// with 31 spend, so MoM between them is 0.
func TestBuildReportLeapYearFebruary(t *testing.T) {
	window := TimeWindow{
		Start: monthTime(2028, time.February),
		End:   monthTime(2028, time.March),
		Months: []time.Time{
			monthTime(2028, time.February),
			monthTime(2028, time.March),
		},
	}
	res := cost.MonthlyAccountServiceResult{
		PayerAccountID: payerID,
		Currency:       "USD",
		Rows: []cost.MonthlyGroupedCost{
			{Month: "2028-02", AccountID: payerID, Service: "Amazon S3", Amount: 29},
			{Month: "2028-03", AccountID: payerID, Service: "Amazon S3", Amount: 31},
		},
	}
	rep, err := BuildReport("example-payer", res, window, nil)
	if err != nil {
		t.Fatal(err)
	}
	feb, mar := rep.Months[0], rep.Months[1]
	if feb.DailyAverage != 1 || mar.DailyAverage != 1 {
		t.Fatalf("daily averages = %v/%v, want 1/1 (leap-year Feb has 29 days)", feb.DailyAverage, mar.DailyAverage)
	}
	if mar.MoMPct == nil || *mar.MoMPct != 0 {
		t.Errorf("mar MoM = %v, want 0", mar.MoMPct)
	}
}
