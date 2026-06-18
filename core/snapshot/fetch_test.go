package snapshot

import (
	"context"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ectypes "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/aws/aws-sdk-go-v2/service/rds"
	rdstypes "github.com/aws/aws-sdk-go-v2/service/rds/types"
)

type fakeRegionLister struct {
	regions []string
}

func (f fakeRegionLister) ListEnabledRegions(_ context.Context, _ aws.Config, _ []string) ([]string, error) {
	return f.regions, nil
}

type fakeEBSLister struct {
	records []Record
}

func (f fakeEBSLister) ListEBSSnapshots(
	_ context.Context,
	_ aws.Config,
	region, accountID string,
	_ time.Time,
	_ float64,
) ([]Record, error) {
	out := make([]Record, 0, len(f.records))
	for _, rec := range f.records {
		if rec.Region == region && rec.AccountID == accountID {
			out = append(out, rec)
		}
	}
	return out, nil
}

type fakeRDSLister struct {
	records []Record
}

func (f fakeRDSLister) ListRDSSnapshots(
	_ context.Context,
	_ aws.Config,
	region, accountID string,
	_ time.Time,
	_ float64,
) ([]Record, error) {
	out := make([]Record, 0, len(f.records))
	for _, rec := range f.records {
		if rec.Region == region && rec.AccountID == accountID {
			out = append(out, rec)
		}
	}
	return out, nil
}

func TestFetchAggregatesRecordsAndSummary(t *testing.T) {
	now := time.Date(2026, 6, 17, 12, 0, 0, 0, time.UTC)
	ebs := Record{
		AccountID:  "111111111111",
		Region:     "us-east-1",
		Kind:       KindEBSSnapshot,
		ResourceID: "snap-old",
		SizeGiB:    100,
	}
	rds := Record{
		AccountID:  "111111111111",
		Region:     "us-east-1",
		Kind:       KindRDSSnapshot,
		ResourceID: "rds-old",
		SizeGiB:    200,
	}

	result, err := Fetch(context.Background(), Query{
		Targets:   []AccountTarget{{AccountID: "111111111111"}},
		OlderThan: 180 * 24 * time.Hour,
		Types:     []Kind{KindEBSSnapshot, KindRDSSnapshot, KindRDSClusterSnapshot},
		Now:       now,
		regionLister: fakeRegionLister{regions: []string{"us-east-1"}},
		ebsLister:    fakeEBSLister{records: []Record{ebs}},
		rdsLister:    fakeRDSLister{records: []Record{rds}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Records) != 2 {
		t.Fatalf("records = %d, want 2", len(result.Records))
	}
	if result.Summary.TotalCount != 2 {
		t.Fatalf("summary count = %d", result.Summary.TotalCount)
	}
	if result.Summary.OlderThanDays != 180 {
		t.Fatalf("older than days = %d", result.Summary.OlderThanDays)
	}
}

func TestEBSSnapshotRecordFiltersByAgeAndSize(t *testing.T) {
	cutoff := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	old := ectypes.Snapshot{
		SnapshotId:  aws.String("snap-old"),
		VolumeId:    aws.String("vol-1"),
		StartTime:   aws.Time(time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)),
		VolumeSize:  aws.Int32(100),
		StorageTier: ectypes.StorageTierStandard,
	}
	rec, ok := ebsSnapshotRecord(old, "111111111111", "us-east-1", cutoff, 0)
	if !ok {
		t.Fatal("expected old snapshot to match")
	}
	if rec.SizeGiB != 100 {
		t.Fatalf("size = %v", rec.SizeGiB)
	}

	newSnap := old
	newSnap.StartTime = aws.Time(time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC))
	if _, ok := ebsSnapshotRecord(newSnap, "111111111111", "us-east-1", cutoff, 0); ok {
		t.Fatal("expected new snapshot to be filtered out")
	}

	small := old
	small.VolumeSize = aws.Int32(1)
	if _, ok := ebsSnapshotRecord(small, "111111111111", "us-east-1", cutoff, 10); ok {
		t.Fatal("expected small snapshot to be filtered out")
	}
}

func TestListEBSSnapshotsPagination(t *testing.T) {
	client := &fakeEC2{
		snapshotPages: [][]ectypes.Snapshot{
			{
				{
					SnapshotId: aws.String("snap-1"),
					VolumeId:   aws.String("vol-1"),
					StartTime:  aws.Time(time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)),
					VolumeSize: aws.Int32(10),
				},
			},
			{
				{
					SnapshotId: aws.String("snap-2"),
					VolumeId:   aws.String("vol-2"),
					StartTime:  aws.Time(time.Date(2025, 2, 1, 0, 0, 0, 0, time.UTC)),
					VolumeSize: aws.Int32(20),
				},
			},
		},
	}
	cutoff := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	records, err := listEBSSnapshotsWithClient(context.Background(), client, "111111111111", "us-east-1", cutoff, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 2 {
		t.Fatalf("records = %d, want 2", len(records))
	}
}

type fakeEC2 struct {
	snapshotPages [][]ectypes.Snapshot
	page          int
}

func (f *fakeEC2) DescribeRegions(
	_ context.Context,
	_ *ec2.DescribeRegionsInput,
	_ ...func(*ec2.Options),
) (*ec2.DescribeRegionsOutput, error) {
	return &ec2.DescribeRegionsOutput{
		Regions: []ectypes.Region{{RegionName: aws.String("us-east-1")}},
	}, nil
}

func (f *fakeEC2) DescribeSnapshots(
	_ context.Context,
	params *ec2.DescribeSnapshotsInput,
	_ ...func(*ec2.Options),
) (*ec2.DescribeSnapshotsOutput, error) {
	if f.page >= len(f.snapshotPages) {
		return &ec2.DescribeSnapshotsOutput{}, nil
	}
	out := &ec2.DescribeSnapshotsOutput{Snapshots: f.snapshotPages[f.page]}
	f.page++
	if f.page < len(f.snapshotPages) {
		out.NextToken = aws.String("next")
	}
	_ = params
	return out, nil
}

func TestListEnabledRegionsUsesRequestedList(t *testing.T) {
	regions, err := listEnabledRegionsWithClient(context.Background(), &fakeEC2{}, []string{"eu-west-1", "us-east-1"})
	if err != nil {
		t.Fatal(err)
	}
	if len(regions) != 2 || regions[0] != "eu-west-1" {
		t.Fatalf("regions = %#v", regions)
	}
}

func TestRDSSnapshotRecord(t *testing.T) {
	cutoff := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	snap := rdstypes.DBSnapshot{
		DBSnapshotIdentifier: aws.String("db-snap-1"),
		DBInstanceIdentifier: aws.String("db-1"),
		SnapshotCreateTime:   aws.Time(time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)),
		AllocatedStorage:     aws.Int32(100),
		SnapshotType:         aws.String("manual"),
	}
	rec, ok := rdsSnapshotRecord(snap, "111111111111", "us-east-1", cutoff, 0)
	if !ok {
		t.Fatal("expected match")
	}
	if rec.Kind != KindRDSSnapshot {
		t.Fatalf("kind = %q", rec.Kind)
	}
}

func TestListRDSSnapshotsWithClient(t *testing.T) {
	client := &fakeRDS{
		dbSnapshots: []rdstypes.DBSnapshot{
			{
				DBSnapshotIdentifier: aws.String("db-snap-1"),
				DBInstanceIdentifier: aws.String("db-1"),
				SnapshotCreateTime:   aws.Time(time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)),
				AllocatedStorage:     aws.Int32(100),
			},
		},
	}
	cutoff := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	records, err := listRDSSnapshotsWithClient(context.Background(), client, "111111111111", "us-east-1", cutoff, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 {
		t.Fatalf("records = %d", len(records))
	}
}

type fakeRDS struct {
	dbSnapshots      []rdstypes.DBSnapshot
	clusterSnapshots []rdstypes.DBClusterSnapshot
}

func (f *fakeRDS) DescribeDBSnapshots(
	_ context.Context,
	_ *rds.DescribeDBSnapshotsInput,
	_ ...func(*rds.Options),
) (*rds.DescribeDBSnapshotsOutput, error) {
	return &rds.DescribeDBSnapshotsOutput{DBSnapshots: f.dbSnapshots}, nil
}

func (f *fakeRDS) DescribeDBClusterSnapshots(
	_ context.Context,
	_ *rds.DescribeDBClusterSnapshotsInput,
	_ ...func(*rds.Options),
) (*rds.DescribeDBClusterSnapshotsOutput, error) {
	return &rds.DescribeDBClusterSnapshotsOutput{DBClusterSnapshots: f.clusterSnapshots}, nil
}
