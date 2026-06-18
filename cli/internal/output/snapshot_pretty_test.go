package output

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/openshift-online/finops-tools/core/snapshot"
)

func TestWriteSnapshotListResultPretty(t *testing.T) {
	var buf bytes.Buffer
	r := snapshot.Result{
		Records: []snapshot.Record{
			{
				AccountID:               "111111111111",
				Region:                  "us-east-1",
				Kind:                    snapshot.KindEBSSnapshot,
				ResourceID:              "snap-abc",
				SourceResourceID:        "vol-1",
				CreatedAt:               time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
				AgeDays:                 400,
				SizeGiB:                 100,
				EstimatedMonthlyCostUSD: 5,
				CostBasis:               snapshot.CostBasisVolumeSizeEstimate,
			},
		},
		Summary: snapshot.Summary{
			TotalCount:              1,
			EstimatedMonthlyCostUSD: 5,
			OlderThanDays:           365,
			ByKind: []snapshot.KindSummary{
				{Kind: snapshot.KindEBSSnapshot, Count: 1, EstimatedMonthlyCostUSD: 5},
			},
			CostDisclaimer: "Estimates use volume or allocated size; actual EBS snapshot billing may be lower.",
		},
	}
	if err := WriteSnapshotListResult(&buf, FormatPrettyPrint, r); err != nil {
		t.Fatal(err)
	}
	out := stripANSI(buf.String())
	for _, want := range []string{
		"Snapshots (older than 365 days)",
		"ACCOUNT",
		"REGION",
		"SNAPSHOT ID",
		"USD 5.00",
		"ebs-snapshot",
		"snap-abc",
		"400d",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\n%s", want, out)
		}
	}
}

func TestWriteSnapshotListResultJSON(t *testing.T) {
	var buf bytes.Buffer
	r := snapshot.Result{Summary: snapshot.Summary{OlderThanDays: 90}}
	if err := WriteSnapshotListResult(&buf, FormatJSON, r); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), `"older_than_days": 90`) {
		t.Fatalf("json = %s", buf.String())
	}
}

func TestWriteSnapshotListResultCSV(t *testing.T) {
	var buf bytes.Buffer
	r := snapshot.Result{
		Records: []snapshot.Record{
			{
				AccountID:               "111111111111",
				Region:                  "us-east-1",
				Kind:                    snapshot.KindEBSSnapshot,
				ResourceID:              "snap-abc",
				CreatedAt:               time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
				AgeDays:                 10,
				SizeGiB:                 1,
				EstimatedMonthlyCostUSD: 0.05,
				CostBasis:               snapshot.CostBasisVolumeSizeEstimate,
			},
		},
	}
	if err := WriteSnapshotListResult(&buf, FormatCSV, r); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "account_id,region,kind") {
		t.Fatalf("csv header missing: %s", out)
	}
	if !strings.Contains(out, "snap-abc") {
		t.Fatalf("csv row missing snapshot: %s", out)
	}
}

func TestWriteSnapshotListResultCSVFormulaInjection(t *testing.T) {
	var buf bytes.Buffer
	r := snapshot.Result{
		Records: []snapshot.Record{
			{
				AccountID:        "=cmd",
				Region:           "+evil",
				Kind:             snapshot.KindEBSSnapshot,
				ResourceID:       "-snap",
				SourceResourceID: "@src",
				CreatedAt:        time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
				AgeDays:          1,
				SizeGiB:          1,
				StorageTier:      "=tier",
				SnapshotType:     "+type",
				Description:      "@desc",
			},
		},
	}
	if err := WriteSnapshotListResult(&buf, FormatCSV, r); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, want := range []string{
		"'=cmd",
		"'+evil",
		"'-snap",
		"'@src",
		"'=tier",
		"'+type",
		"'@desc",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("csv missing escaped field %q\n%s", want, out)
		}
	}
}
