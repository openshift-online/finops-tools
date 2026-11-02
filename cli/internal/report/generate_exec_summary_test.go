// generate_exec_summary_test.go tests the exec-summary generator, catalog entry, and renderer.
package report

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/openshift-online/finops-tools/core/cost"
	"github.com/openshift-online/finops-tools/core/report/execsummary"
)

func TestTemplateExecSummaryRegistered(t *testing.T) {
	if _, err := ParseTemplate("exec-summary"); err != nil {
		t.Fatalf("ParseTemplate: %v", err)
	}
	found := false
	for _, tpl := range Templates() {
		if tpl.Name == TemplateExecSummary {
			found = true
		}
	}
	if !found {
		t.Error("exec-summary missing from Templates()")
	}
	if _, err := GeneratorFor(TemplateExecSummary); err != nil {
		t.Fatalf("GeneratorFor: %v", err)
	}
}

func TestExecSummaryGeneratorValidate(t *testing.T) {
	g := execSummaryGenerator{}

	if err := g.Validate(GenerateInput{Format: FormatHTML, Targets: []cost.AccountTarget{{AccountID: "123456789012"}}}); err != nil {
		t.Errorf("valid input rejected: %v", err)
	}
	if err := g.Validate(GenerateInput{Format: "json", Targets: []cost.AccountTarget{{AccountID: "123456789012"}}}); err == nil {
		t.Error("non-html format should fail")
	}
	if err := g.Validate(GenerateInput{Format: FormatHTML}); err == nil {
		t.Error("missing targets should fail")
	}
}

func TestExecSummaryGeneratorGenerateRendersHTML(t *testing.T) {
	defer func(f func(context.Context, cost.CostQuery) (cost.MonthlyAccountServiceResult, error)) {
		fetchMonthlyAccountService = f
	}(fetchMonthlyAccountService)

	now := time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC)
	fetchMonthlyAccountService = func(_ context.Context, q cost.CostQuery) (cost.MonthlyAccountServiceResult, error) {
		// The generator must scope the query to the window months.
		if q.Range.Start.Format("2006-01-02") != "2026-02-01" {
			return cost.MonthlyAccountServiceResult{}, fmt.Errorf("range start = %s, want 2026-02-01", q.Range.Start.Format("2006-01-02"))
		}
		if q.Range.End.Format("2006-01-02") != "2026-07-01" {
			return cost.MonthlyAccountServiceResult{}, fmt.Errorf("range end = %s, want 2026-07-01", q.Range.End.Format("2006-01-02"))
		}
		return cost.MonthlyAccountServiceResult{
			PayerAccountID: "123456789012",
			Currency:       "USD",
			Rows: []cost.MonthlyGroupedCost{
				{Month: "2026-06", AccountID: "123456789012", Service: "Amazon S3", Amount: 30},
				{Month: "2026-06", AccountID: "111111111111", Service: "Amazon EC2", Amount: 60},
			},
		}, nil
	}

	var buf bytes.Buffer
	in := GenerateInput{
		Format:  FormatHTML,
		Out:     &buf,
		Now:     now,
		Months:  5,
		Targets: []cost.AccountTarget{
			{AccountID: "123456789012", DisplayName: "Payer"},
			{AccountID: "111111111111", PayerAccountID: "123456789012", DisplayName: "Member A"},
		},
	}
	if err := (execSummaryGenerator{}).Generate(context.Background(), in); err != nil {
		t.Fatal(err)
	}

	out := buf.String()
	for _, want := range []string{
		"Executive Summary",
		"Payer",
		"Monthly Total",
		"MoM Growth",
		"90.00",
		"Amazon EC2",
		"Amazon S3",
		"2026-06",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q", want)
		}
	}
}

func TestExecSummaryGeneratorDefaultsToFiveMonths(t *testing.T) {
	defer func(f func(context.Context, cost.CostQuery) (cost.MonthlyAccountServiceResult, error)) {
		fetchMonthlyAccountService = f
	}(fetchMonthlyAccountService)

	var gotMonths int
	fetchMonthlyAccountService = func(_ context.Context, q cost.CostQuery) (cost.MonthlyAccountServiceResult, error) {
		gotMonths = len(execsummary.MonthsInWindow(q.Range.Start, q.Range.End.AddDate(0, 0, -1)))
		return cost.MonthlyAccountServiceResult{PayerAccountID: "123456789012", Currency: "USD"}, nil
	}

	in := GenerateInput{
		Format:  FormatHTML,
		Out:     &bytes.Buffer{},
		Now:     time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC),
		Targets: []cost.AccountTarget{{AccountID: "123456789012"}},
	}
	if err := (execSummaryGenerator{}).Generate(context.Background(), in); err != nil {
		t.Fatal(err)
	}
	if gotMonths != 5 {
		t.Errorf("months = %d, want 5 by default", gotMonths)
	}
}

func TestExecSummaryGeneratorFetchErrorPropagates(t *testing.T) {
	defer func(f func(context.Context, cost.CostQuery) (cost.MonthlyAccountServiceResult, error)) {
		fetchMonthlyAccountService = f
	}(fetchMonthlyAccountService)

	fetchMonthlyAccountService = func(context.Context, cost.CostQuery) (cost.MonthlyAccountServiceResult, error) {
		return cost.MonthlyAccountServiceResult{}, fmt.Errorf("boom")
	}

	in := GenerateInput{
		Format:  FormatHTML,
		Out:     &bytes.Buffer{},
		Now:     time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC),
		Targets: []cost.AccountTarget{{AccountID: "123456789012"}},
	}
	err := (execSummaryGenerator{}).Generate(context.Background(), in)
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("err = %v, want propagated fetch error", err)
	}
}

func TestRenderExecSummaryHTMLEscapingAndKPIs(t *testing.T) {
	mom := -10.5
	rep := execsummary.Report{
		PayerAlias:     "rh-control",
		PayerAccountID: "123456789012",
		Currency:       "USD",
		StartMonth:     "2026-02",
		EndMonth:       "2026-06",
		Months: []execsummary.MonthSummary{
			{Month: "2026-05", Total: 100, DailyAverage: 100.0 / 31},
			{Month: "2026-06", Total: 90, DailyAverage: 3, MoMPct: &mom},
		},
		Accounts: []execsummary.AccountSummary{
			{
				AccountID:   "111111111111",
				DisplayName: "Member <script>alert(1)</script>",
				Total:       60,
				MonthlyTotals: map[string]float64{
					"2026-06": 60,
				},
				Services: []execsummary.ServiceMonthCost{
					{Month: "2026-06", Service: "Amazon EC2 <b>", Amount: 60},
				},
			},
		},
	}

	var buf bytes.Buffer
	if err := RenderExecSummaryHTML(&buf, rep); err != nil {
		t.Fatal(err)
	}
	out := buf.String()

	if strings.Contains(out, "<script>alert(1)</script>") {
		t.Error("account display name not escaped")
	}
	if strings.Contains(out, "Amazon EC2 <b>") {
		t.Error("service name not escaped")
	}
	if !strings.Contains(out, "&lt;script&gt;") {
		t.Error("expected escaped account name in output")
	}
	if !strings.Contains(out, "N/A") {
		t.Error("first displayed month MoM should render N/A")
	}
	if !strings.Contains(out, "-10.5%") {
		t.Error("MoM percentage missing")
	}
	if !strings.Contains(out, "USD") {
		t.Error("currency missing")
	}
}
