package output

import (
	"testing"

	"github.com/openshift-online/finops-tools/core/snapshot"
)

func TestBuildSnapshotSummaryLinesHidesRedundantEBSLines(t *testing.T) {
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
		t.Fatalf("lines = %d, want count + billed only", len(lines))
	}
	if lines[1].label != "EBS snapshot storage (May 2026)" {
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
	if len(lines) != 3 {
		t.Fatalf("lines = %d, want 3", len(lines))
	}
	if lines[2].label != "Estimated cost (listed snapshots only)" {
		t.Fatalf("label = %q", lines[2].label)
	}
	if lines[2].value != "USD 1,000.00" {
		t.Fatalf("value = %q", lines[2].value)
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
