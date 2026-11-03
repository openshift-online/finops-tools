package output

import (
	"bytes"
	"strings"
	"testing"

	"github.com/openshift-online/finops-tools/core/snapshot"
)

func TestBuildSnapshotSummaryLinesShowsAttributedWhenListedIsMostOfAccount(t *testing.T) {
	summary := snapshot.Summary{
		EstimatedMonthlyCostUSD:       1263.28,
		EBSEstimatedMonthlyRunRateUSD: 1263.28,
		OlderThanDays:                 365,
		TotalCount:                    8187,
		BilledCosts: []snapshot.AccountBilledSnapshotCosts{
			{
				Period: snapshot.BilledSnapshotPeriod{
					StartDate: "2026-05-01",
					EndDate:   "2026-05-31",
				},
				EBSSnapshotUSD: 4798.21,
			},
		},
		RegionalBilledCosts: []snapshot.AccountBilledSnapshotCosts{
			{
				Period: snapshot.BilledSnapshotPeriod{
					StartDate: "2026-05-01",
					EndDate:   "2026-05-31",
				},
				EBSSnapshotUSD: 4798.21,
			},
		},
	}
	lines := buildSnapshotSummaryLines(summary, newSnapshotCostContext(summary))

	if len(lines) != 6 {
		t.Fatalf("lines = %d, want count + attributed + account-wide and regional billed", len(lines))
	}
	if lines[1].label != "Attributed cost (listed snapshots, May 2026)" {
		t.Fatalf("label = %q", lines[1].label)
	}
	if lines[1].value != "USD 4,798.21" {
		t.Fatalf("value = %q", lines[1].value)
	}
	if lines[2].label != "Billed EBS snapshot storage (account-wide, May 2026)" {
		t.Fatalf("billed label = %q", lines[2].label)
	}
	if lines[2].value != "USD 4,798.21" {
		t.Fatalf("billed value = %q", lines[2].value)
	}
}

func TestBuildSnapshotSummaryLinesShowsAttributedWhenPartial(t *testing.T) {
	summary := snapshot.Summary{
		EstimatedMonthlyCostUSD:       200,
		EBSEstimatedMonthlyRunRateUSD: 1000,
		OlderThanDays:                 365,
		TotalCount:                    10,
		BilledCosts: []snapshot.AccountBilledSnapshotCosts{
			{
				Period:         snapshot.BilledSnapshotPeriod{StartDate: "2026-05-01"},
				EBSSnapshotUSD: 5000,
			},
		},
		RegionalBilledCosts: []snapshot.AccountBilledSnapshotCosts{
			{
				Period:         snapshot.BilledSnapshotPeriod{StartDate: "2026-05-01"},
				EBSSnapshotUSD: 5000,
			},
		},
	}
	lines := buildSnapshotSummaryLines(summary, newSnapshotCostContext(summary))
	if len(lines) != 6 {
		t.Fatalf("lines = %d, want 6", len(lines))
	}
	if lines[1].label != "Attributed cost (listed snapshots, May 2026)" {
		t.Fatalf("label = %q", lines[1].label)
	}
	if lines[1].value != "USD 1,000.00" {
		t.Fatalf("value = %q", lines[1].value)
	}
	if lines[2].label != "Billed EBS snapshot storage (account-wide, May 2026)" {
		t.Fatalf("billed label = %q", lines[2].label)
	}
	if lines[2].value != "USD 5,000.00" {
		t.Fatalf("billed value = %q", lines[2].value)
	}
}

func TestBuildSnapshotSummaryLinesShowsNeutralLabelWhenMixed(t *testing.T) {
	summary := snapshot.Summary{
		ScanScopes:                          []snapshot.AccountScanScope{{RDSCostEstimationIncomplete: true}},
		EstimatedMonthlyCostUSD:             700,
		EBSEstimatedMonthlyRunRateUSD:       500,
		RDSBackupEstimatedMonthlyRunRateUSD: 200,
		OlderThanDays:                       365,
		TotalCount:                          10,
		BilledCosts: []snapshot.AccountBilledSnapshotCosts{
			{
				Period:         snapshot.BilledSnapshotPeriod{StartDate: "2026-05-01"},
				EBSSnapshotUSD: 5000,
				RDSBackupUSD:   0,
			},
		},
		RegionalBilledCosts: []snapshot.AccountBilledSnapshotCosts{
			{
				Period:         snapshot.BilledSnapshotPeriod{StartDate: "2026-05-01"},
				EBSSnapshotUSD: 5000,
				RDSBackupUSD:   0,
			},
		},
		ByKind: []snapshot.KindSummary{
			{Kind: snapshot.KindEBSSnapshot, Count: 5, EstimatedMonthlyCostUSD: 500},
			{Kind: snapshot.KindRDSSnapshot, Count: 5, EstimatedMonthlyCostUSD: 200},
		},
	}
	lines := buildSnapshotSummaryLines(summary, newSnapshotCostContext(summary))
	if len(lines) != 6 {
		t.Fatalf("lines = %d, want 6", len(lines))
	}
	if lines[1].label != "Cost (listed snapshots, May 2026)" {
		t.Fatalf("label = %q", lines[1].label)
	}
	if lines[1].value != "USD 5,200.00" {
		t.Fatalf("value = %q", lines[1].value)
	}
	if lines[2].label != "Billed EBS snapshot storage (account-wide, May 2026)" {
		t.Fatalf("billed label = %q", lines[2].label)
	}
	if lines[2].value != "USD 5,000.00" {
		t.Fatalf("billed value = %q", lines[2].value)
	}
}

func TestSnapshotKindCostSuffixReflectsScale(t *testing.T) {
	ctx := newSnapshotCostContext(snapshot.Summary{
		EBSEstimatedMonthlyRunRateUSD: 100,
		BilledCosts: []snapshot.AccountBilledSnapshotCosts{
			{EBSSnapshotUSD: 50},
		},
		RegionalBilledCosts: []snapshot.AccountBilledSnapshotCosts{
			{EBSSnapshotUSD: 50},
		},
	})
	if got := snapshotKindCostSuffix(snapshot.KindEBSSnapshot, ctx); got != "attributed" {
		t.Fatalf("EBS suffix = %q", got)
	}
	if got := snapshotKindCostSuffix(snapshot.KindRDSSnapshot, ctx); got != "estimated" {
		t.Fatalf("RDS suffix = %q", got)
	}
}

func TestSnapshotMonthlyCostColumnHeaderMixed(t *testing.T) {
	ctx := newSnapshotCostContext(snapshot.Summary{
		EBSEstimatedMonthlyRunRateUSD: 100,
		BilledCosts: []snapshot.AccountBilledSnapshotCosts{
			{EBSSnapshotUSD: 50},
		},
		RegionalBilledCosts: []snapshot.AccountBilledSnapshotCosts{
			{EBSSnapshotUSD: 50},
		},
	})
	records := []snapshot.Record{
		{Kind: snapshot.KindEBSSnapshot, EstimatedMonthlyCostUSD: 10},
		{Kind: snapshot.KindRDSSnapshot, EstimatedMonthlyCostUSD: 20},
	}
	if got := snapshotMonthlyCostColumnHeader(records, ctx); got != "COST/MO" {
		t.Fatalf("header = %q, want COST/MO", got)
	}
}

func TestWriteSnapshotByTypeUsesEstimatedSuffixWithoutCE(t *testing.T) {
	var buf bytes.Buffer
	r := snapshot.Result{
		Summary: snapshot.Summary{
			OlderThanDays: 365,
			TotalCount:    1,
			ByKind: []snapshot.KindSummary{
				{Kind: snapshot.KindEBSSnapshot, Count: 1, EstimatedMonthlyCostUSD: 5},
			},
		},
	}
	if err := WriteSnapshotListResult(&buf, FormatPrettyPrint, r); err != nil {
		t.Fatal(err)
	}
	out := stripANSI(buf.String())
	if !strings.Contains(out, "USD 5.00 estimated") {
		t.Fatalf("output missing estimated by-type suffix:\n%s", out)
	}
	if strings.Contains(out, "attributed") {
		t.Fatalf("output should not claim attribution without CE data:\n%s", out)
	}
}

func TestSnapshotRecordMonthlyCostUsesDashForZeroIncremental(t *testing.T) {
	ctx := newSnapshotCostContext(snapshot.Summary{
		EBSEstimatedMonthlyRunRateUSD: 100,
		BilledCosts: []snapshot.AccountBilledSnapshotCosts{
			{EBSSnapshotUSD: 500},
		},
		RegionalBilledCosts: []snapshot.AccountBilledSnapshotCosts{
			{EBSSnapshotUSD: 500},
		},
	})
	got := snapshotRecordMonthlyCost(snapshot.Record{
		Kind:                    snapshot.KindEBSSnapshot,
		EstimatedMonthlyCostUSD: 0,
	}, ctx)
	if got != "—" {
		t.Fatalf("got = %q, want dash", got)
	}
}

func TestScaleRDSCostUsesBilledRunRate(t *testing.T) {
	summary := snapshot.Summary{
		RDSBackupEstimatedMonthlyRunRateUSD: 342,
		BilledCosts: []snapshot.AccountBilledSnapshotCosts{
			{RDSBackupUSD: 0.77},
		},
		RegionalBilledCosts: []snapshot.AccountBilledSnapshotCosts{
			{RDSBackupUSD: 0.77},
		},
	}
	ctx := newSnapshotCostContext(summary)
	if ctx.rdsCEScale <= 0 {
		t.Fatal("expected RDS CE scale")
	}
	got := scaleRDSCost(342, snapshot.KindRDSSnapshot, ctx)
	if got < 0.76 || got > 0.78 {
		t.Fatalf("scaled RDS cost = %v, want ~0.77", got)
	}
}

func TestWriteSnapshotByTypeScalesRDSWithCE(t *testing.T) {
	var buf bytes.Buffer
	r := snapshot.Result{
		Summary: snapshot.Summary{
			OlderThanDays:                       365,
			TotalCount:                          6,
			RDSBackupEstimatedMonthlyRunRateUSD: 342,
			BilledCosts: []snapshot.AccountBilledSnapshotCosts{
				{
					Period:       snapshot.BilledSnapshotPeriod{StartDate: "2026-05-01"},
					RDSBackupUSD: 0.77,
				},
			},
			RegionalBilledCosts: []snapshot.AccountBilledSnapshotCosts{
				{
					Period:       snapshot.BilledSnapshotPeriod{StartDate: "2026-05-01"},
					RDSBackupUSD: 0.77,
				},
			},
			ByKind: []snapshot.KindSummary{
				{Kind: snapshot.KindRDSSnapshot, Count: 6, EstimatedMonthlyCostUSD: 342},
			},
		},
	}
	if err := WriteSnapshotListResult(&buf, FormatPrettyPrint, r); err != nil {
		t.Fatal(err)
	}
	out := stripANSI(buf.String())
	if !strings.Contains(out, "USD 0.77 attributed") {
		t.Fatalf("output missing scaled RDS by-type total:\n%s", out)
	}
	if strings.Contains(out, "USD 342.00") {
		t.Fatalf("output still shows unscaled RDS estimate:\n%s", out)
	}
}

func TestSnapshotCostsUseRegionalBillingAndCompareAccountWideTotals(t *testing.T) {
	summary := snapshot.Summary{
		TotalCount: 2, OlderThanDays: 365, EstimatedMonthlyCostUSD: 60,
		EBSEstimatedMonthlyRunRateUSD: 100, RDSBackupEstimatedMonthlyRunRateUSD: 100,
		BilledCosts: []snapshot.AccountBilledSnapshotCosts{{
			AccountID: "111111111111", Period: snapshot.BilledSnapshotPeriod{StartDate: "2026-05-01"},
			EBSSnapshotUSD: 1000, RDSBackupUSD: 500,
		}},
		RegionalBilledCosts: []snapshot.AccountBilledSnapshotCosts{{
			AccountID: "111111111111", Period: snapshot.BilledSnapshotPeriod{StartDate: "2026-05-01"},
			EBSSnapshotUSD: 100, RDSBackupUSD: 50,
		}},
		ByKind: []snapshot.KindSummary{
			{Kind: snapshot.KindEBSSnapshot, Count: 1, EstimatedMonthlyCostUSD: 20},
			{Kind: snapshot.KindRDSSnapshot, Count: 1, EstimatedMonthlyCostUSD: 40},
		},
	}
	records := []snapshot.Record{
		{AccountID: "111111111111", Region: "us-east-1", Kind: snapshot.KindEBSSnapshot, ResourceID: "snap-ebs", EstimatedMonthlyCostUSD: 20},
		{AccountID: "111111111111", Region: "us-east-1", Kind: snapshot.KindRDSSnapshot, ResourceID: "snap-rds", EstimatedMonthlyCostUSD: 40},
	}
	ctx := newSnapshotCostContext(summary)
	for _, rec := range records {
		if got := snapshotRecordMonthlyCost(rec, ctx); got != "USD 20.00" {
			t.Fatalf("%s attributed cost = %s, want regional share USD 20.00", rec.Kind, got)
		}
	}
	lines := buildSnapshotSummaryLines(summary, ctx)
	want := map[string]string{
		"Attributed cost (listed snapshots, May 2026)":            "USD 40.00",
		"Billed EBS snapshot storage (account-wide, May 2026)":    "USD 1,000.00",
		"Billed RDS backup storage (account-wide, May 2026)":      "USD 500.00",
		"Billed EBS snapshot storage (scanned regions, May 2026)": "USD 100.00 (10.0% of account-wide)",
		"Billed RDS backup storage (scanned regions, May 2026)":   "USD 50.00 (10.0% of account-wide)",
	}
	for _, line := range lines {
		if value, ok := want[line.label]; ok {
			if line.value != value {
				t.Fatalf("%s = %s, want %s", line.label, line.value, value)
			}
			delete(want, line.label)
		}
	}
	if len(want) != 0 {
		t.Fatalf("missing cost comparison lines: %v", want)
	}
	var buf bytes.Buffer
	if err := WriteSnapshotListResult(&buf, FormatPrettyPrint, snapshot.Result{Summary: summary, Records: records}); err != nil {
		t.Fatal(err)
	}
	if strings.Count(buf.String(), "USD 20.00 attributed") != 2 || !strings.Contains(buf.String(), "snap-ebs") || strings.Contains(buf.String(), "USD 200.00") {
		t.Fatalf("per-type and detail costs must use regional billing: %s", buf.String())
	}
}

func TestSnapshotAttributionRequiresRegionalBilling(t *testing.T) {
	summary := snapshot.Summary{
		EstimatedMonthlyCostUSD: 20, EBSEstimatedMonthlyRunRateUSD: 100,
		BilledCosts: []snapshot.AccountBilledSnapshotCosts{{EBSSnapshotUSD: 1000}},
	}
	ctx := newSnapshotCostContext(summary)
	if ctx.ebsAttributed || listedSnapshotCostUSD(summary, ctx) != 20 || scaleEBSCost(20, snapshot.KindEBSSnapshot, ctx) != 20 {
		t.Fatalf("account-wide billing must not replace unavailable regional billing: %+v", ctx)
	}
}

func TestZeroRegionalBillDoesNotFallBackToEstimates(t *testing.T) {
	summary := snapshot.Summary{
		TotalCount:              2,
		EstimatedMonthlyCostUSD: 60, EBSEstimatedMonthlyRunRateUSD: 100, RDSBackupEstimatedMonthlyRunRateUSD: 100,
		BilledCosts:         []snapshot.AccountBilledSnapshotCosts{{EBSSnapshotUSD: 1000, RDSBackupUSD: 500}},
		RegionalBilledCosts: []snapshot.AccountBilledSnapshotCosts{{}},
		ByKind: []snapshot.KindSummary{
			{Kind: snapshot.KindEBSSnapshot, EstimatedMonthlyCostUSD: 20},
			{Kind: snapshot.KindRDSSnapshot, EstimatedMonthlyCostUSD: 40},
		},
	}
	ctx := newSnapshotCostContext(summary)
	if !ctx.ebsAttributed || !ctx.rdsAttributed || listedSnapshotCostUSD(summary, ctx) != 0 || scaleEBSCost(20, snapshot.KindEBSSnapshot, ctx) != 0 || scaleRDSCost(40, snapshot.KindRDSSnapshot, ctx) != 0 {
		t.Fatalf("confirmed zero regional spending must stay zero: %+v", ctx)
	}
	if got := regionalSnapshotCostComparison(ctx.regionalEBS, ctx.billedEBS); got != "USD 0.00 (0.0% of account-wide)" {
		t.Fatal(got)
	}
	if got := snapshotRecordMonthlyCost(snapshot.Record{Kind: snapshot.KindEBSSnapshot, EstimatedMonthlyCostUSD: 20}, ctx); got != "USD 0.00" {
		t.Fatalf("zero billed cost should not claim there are no incremental blocks: %s", got)
	}
	lines := buildSnapshotSummaryLines(summary, ctx)
	if !strings.HasPrefix(lines[1].label, "Attributed cost") || lines[1].value != "USD 0.00" {
		t.Fatalf("zero attribution should remain visible: %+v", lines)
	}
}

func TestIncompleteRDSRunRateRetainsEstimates(t *testing.T) {
	summary := snapshot.Summary{
		RDSBackupEstimatedMonthlyRunRateUSD: 100,
		RegionalBilledCosts:                 []snapshot.AccountBilledSnapshotCosts{{RDSBackupUSD: 1000}},
		ScanScopes:                          []snapshot.AccountScanScope{{RDSCostEstimationIncomplete: true}},
	}
	ctx := newSnapshotCostContext(summary)
	if ctx.rdsAttributed || scaleRDSCost(20, snapshot.KindRDSSnapshot, ctx) != 20 {
		t.Fatalf("incomplete RDS denominator must not scale estimates: %+v", ctx)
	}
}
