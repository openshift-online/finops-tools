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
	}
	lines := buildSnapshotSummaryLines(summary, newSnapshotCostContext(summary))

	if len(lines) != 2 {
		t.Fatalf("lines = %d, want count + attributed only", len(lines))
	}
	if lines[1].label != "Attributed cost (listed snapshots, May 2026)" {
		t.Fatalf("label = %q", lines[1].label)
	}
	if lines[1].value != "USD 4,798.21" {
		t.Fatalf("value = %q", lines[1].value)
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
	}
	lines := buildSnapshotSummaryLines(summary, newSnapshotCostContext(summary))
	if len(lines) != 2 {
		t.Fatalf("lines = %d, want 2", len(lines))
	}
	if lines[1].label != "Attributed cost (listed snapshots, May 2026)" {
		t.Fatalf("label = %q", lines[1].label)
	}
	if lines[1].value != "USD 1,000.00" {
		t.Fatalf("value = %q", lines[1].value)
	}
}

func TestBuildSnapshotSummaryLinesShowsNeutralLabelWhenMixed(t *testing.T) {
	summary := snapshot.Summary{
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
		ByKind: []snapshot.KindSummary{
			{Kind: snapshot.KindEBSSnapshot, Count: 5, EstimatedMonthlyCostUSD: 500},
			{Kind: snapshot.KindRDSSnapshot, Count: 5, EstimatedMonthlyCostUSD: 200},
		},
	}
	lines := buildSnapshotSummaryLines(summary, newSnapshotCostContext(summary))
	if len(lines) != 2 {
		t.Fatalf("lines = %d, want 2", len(lines))
	}
	if lines[1].label != "Cost (listed snapshots, May 2026)" {
		t.Fatalf("label = %q", lines[1].label)
	}
	if lines[1].value != "USD 5,200.00" {
		t.Fatalf("value = %q", lines[1].value)
	}
}

func TestSnapshotKindCostSuffixReflectsScale(t *testing.T) {
	ctx := newSnapshotCostContext(snapshot.Summary{
		EBSEstimatedMonthlyRunRateUSD: 100,
		BilledCosts: []snapshot.AccountBilledSnapshotCosts{
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
