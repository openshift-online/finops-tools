package snapshot

import (
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	ectypes "github.com/aws/aws-sdk-go-v2/service/ec2/types"
)

func TestComputeEBSSnapshotChargesIncrementalChain(t *testing.T) {
	full100 := int64(100 * 1024 * 1024 * 1024)
	full150 := int64(150 * 1024 * 1024 * 1024)
	snapshots := []ectypes.Snapshot{
		{
			SnapshotId:             aws.String("snap-1"),
			VolumeId:               aws.String("vol-1"),
			StartTime:              aws.Time(mustTime("2025-01-01")),
			VolumeSize:             aws.Int32(200),
			FullSnapshotSizeInBytes: &full100,
			StorageTier:            ectypes.StorageTierStandard,
		},
		{
			SnapshotId:             aws.String("snap-2"),
			VolumeId:               aws.String("vol-1"),
			StartTime:              aws.Time(mustTime("2025-02-01")),
			VolumeSize:             aws.Int32(200),
			FullSnapshotSizeInBytes: &full150,
			StorageTier:            ectypes.StorageTierStandard,
		},
	}
	charges := computeEBSSnapshotCharges(snapshots)
	if len(charges) != 2 {
		t.Fatalf("charges = %d", len(charges))
	}
	if charges["snap-1"].IncrementalGiB != 100 {
		t.Fatalf("snap-1 incremental = %v", charges["snap-1"].IncrementalGiB)
	}
	if charges["snap-2"].IncrementalGiB != 50 {
		t.Fatalf("snap-2 incremental = %v", charges["snap-2"].IncrementalGiB)
	}
	if charges["snap-1"].MonthlyUSD != 5 {
		t.Fatalf("snap-1 monthly = %v", charges["snap-1"].MonthlyUSD)
	}
	if charges["snap-2"].MonthlyUSD != 2.5 {
		t.Fatalf("snap-2 monthly = %v", charges["snap-2"].MonthlyUSD)
	}
}

func TestComputeEBSSnapshotChargesArchivedSnapshotBilledAtFullSize(t *testing.T) {
	full100 := int64(100 * 1024 * 1024 * 1024)
	full150 := int64(150 * 1024 * 1024 * 1024)
	snapshots := []ectypes.Snapshot{
		{
			SnapshotId:              aws.String("snap-1"),
			VolumeId:                aws.String("vol-1"),
			StartTime:               aws.Time(mustTime("2025-01-01")),
			VolumeSize:              aws.Int32(200),
			FullSnapshotSizeInBytes: &full100,
			StorageTier:             ectypes.StorageTierStandard,
		},
		{
			SnapshotId:              aws.String("snap-2"),
			VolumeId:                aws.String("vol-1"),
			StartTime:               aws.Time(mustTime("2025-02-01")),
			VolumeSize:              aws.Int32(200),
			FullSnapshotSizeInBytes: &full150,
			StorageTier:             ectypes.StorageTierArchive,
		},
	}
	charges := computeEBSSnapshotCharges(snapshots)
	if charges["snap-2"].IncrementalGiB != 150 {
		t.Fatalf("snap-2 incremental = %v, want 150 (full size for archive tier)", charges["snap-2"].IncrementalGiB)
	}
	wantMonthly := 150 * ebsArchiveUSDPerGiBMonth
	if charges["snap-2"].MonthlyUSD != wantMonthly {
		t.Fatalf("snap-2 monthly = %v, want %v", charges["snap-2"].MonthlyUSD, wantMonthly)
	}
}

func TestComputeEBSSnapshotChargesRedundantSnapshotIsZeroCost(t *testing.T) {
	full100 := int64(100 * 1024 * 1024 * 1024)
	snapshots := []ectypes.Snapshot{
		{
			SnapshotId:              aws.String("snap-1"),
			VolumeId:                aws.String("vol-1"),
			StartTime:               aws.Time(mustTime("2025-01-01")),
			VolumeSize:              aws.Int32(100),
			FullSnapshotSizeInBytes: &full100,
		},
		{
			SnapshotId:              aws.String("snap-2"),
			VolumeId:                aws.String("vol-1"),
			StartTime:               aws.Time(mustTime("2025-02-01")),
			VolumeSize:              aws.Int32(100),
			FullSnapshotSizeInBytes: &full100,
		},
	}
	charges := computeEBSSnapshotCharges(snapshots)
	if charges["snap-2"].MonthlyUSD != 0 {
		t.Fatalf("snap-2 monthly = %v, want 0", charges["snap-2"].MonthlyUSD)
	}
}

func TestApplyEBSChargeNeverUsesVolumeSizeFallback(t *testing.T) {
	rec := Record{SizeGiB: 500, StorageTier: "standard"}
	applyEBSChargeToRecord(&rec, ebsSnapshotCharge{}, false)
	if rec.EstimatedMonthlyCostUSD != 0 {
		t.Fatalf("cost = %v, want 0", rec.EstimatedMonthlyCostUSD)
	}
	if rec.CostBasis != CostBasisEBSNoIncremental {
		t.Fatalf("cost basis = %q", rec.CostBasis)
	}
}

func mustTime(s string) time.Time {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		panic(err)
	}
	return t
}
