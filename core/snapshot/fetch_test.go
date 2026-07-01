package snapshot

import (
	"context"
	"errors"
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
) ([]Record, float64, error) {
	out := make([]Record, 0, len(f.records))
	for _, rec := range f.records {
		if rec.Region == region && rec.AccountID == accountID {
			out = append(out, rec)
		}
	}
	return out, 0, nil
}

type fakeRDSLister struct {
	records        []Record
	FreePoolGiB    float64
	TotalBackupGiB float64
	contextErr     error
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

func (f fakeRDSLister) GetRDSRegionContext(
	_ context.Context,
	_ aws.Config,
	_ string,
) (RDSRegionContext, error) {
	if f.contextErr != nil {
		return RDSRegionContext{}, f.contextErr
	}
	return RDSRegionContext{
		FreePoolGiB:      f.FreePoolGiB,
		TotalBackupGiB: f.TotalBackupGiB,
	}, nil
}

func TestFetchSkipsRDSRegionContextWhenFilteredRecordsEmpty(t *testing.T) {
	now := time.Date(2026, 6, 17, 12, 0, 0, 0, time.UTC)
	clusterOnly := Record{
		AccountID:  "111111111111",
		Region:     "us-east-1",
		Kind:       KindRDSClusterSnapshot,
		ResourceID: "cluster-old",
		SizeGiB:    200,
	}

	result, err := Fetch(context.Background(), Query{
		Targets:   []AccountTarget{{AccountID: "111111111111"}},
		OlderThan: 180 * 24 * time.Hour,
		Types:     []Kind{KindRDSSnapshot},
		Now:       now,
		regionLister: fakeRegionLister{regions: []string{"us-east-1"}},
		rdsLister: fakeRDSLister{
			records:        []Record{clusterOnly},
			FreePoolGiB:    0,
			TotalBackupGiB: 1000,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Records) != 0 {
		t.Fatalf("records = %d, want 0 after kind filter", len(result.Records))
	}
	if result.Summary.RDSBackupRegionalExcessGiB != 0 {
		t.Fatalf("regional excess GiB = %v, want 0 for empty filtered region", result.Summary.RDSBackupRegionalExcessGiB)
	}
	if result.Summary.RDSBackupEstimatedMonthlyRunRateUSD != 0 {
		t.Fatalf("regional run rate = %v, want 0 for empty filtered region", result.Summary.RDSBackupEstimatedMonthlyRunRateUSD)
	}
}

func TestFetchPropagatesRDSRegionContextCancellation(t *testing.T) {
	now := time.Date(2026, 6, 17, 12, 0, 0, 0, time.UTC)
	rds := Record{
		AccountID:  "111111111111",
		Region:     "us-east-1",
		Kind:       KindRDSSnapshot,
		ResourceID: "rds-old",
		SizeGiB:    200,
	}

	_, err := Fetch(context.Background(), Query{
		Targets:   []AccountTarget{{AccountID: "111111111111"}},
		OlderThan: 180 * 24 * time.Hour,
		Types:     []Kind{KindRDSSnapshot},
		Now:       now,
		regionLister: fakeRegionLister{regions: []string{"us-east-1"}},
		rdsLister: fakeRDSLister{
			records:    []Record{rds},
			contextErr: context.Canceled,
		},
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Fetch() error = %v, want context.Canceled", err)
	}
}

func TestFetchAggregatesRecordsAndSummary(t *testing.T) {
	now := time.Date(2026, 6, 17, 12, 0, 0, 0, time.UTC)
	ebs := Record{
		AccountID:               "111111111111",
		Region:                  "us-east-1",
		Kind:                    KindEBSSnapshot,
		ResourceID:              "snap-old",
		SizeGiB:                 100,
		EstimatedMonthlyCostUSD: 5,
		CostBasis:               CostBasisVolumeSizeEstimate,
	}
	rds := Record{
		AccountID:  "111111111111",
		Region:     "us-east-1",
		Kind:       KindRDSSnapshot,
		ResourceID: "rds-old",
		SizeGiB:    200,
	}

	result, err := Fetch(context.Background(), Query{
		Targets: []AccountTarget{{AccountID: "111111111111"}},
		OlderThan: 180 * 24 * time.Hour,
		Types: []Kind{KindEBSSnapshot, KindRDSSnapshot, KindRDSClusterSnapshot},
		Now: now,
		regionLister: fakeRegionLister{regions: []string{"us-east-1"}},
		ebsLister:    fakeEBSLister{records: []Record{ebs}},
		rdsLister: fakeRDSLister{
			records:        []Record{rds},
			FreePoolGiB:    500,
			TotalBackupGiB: 200,
		},
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
	if result.Summary.EstimatedMonthlyCostUSD != 5 {
		t.Fatalf("summary cost = %v, want 5 (RDS within free tier)", result.Summary.EstimatedMonthlyCostUSD)
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
	rec, ok := ebsSnapshotRecord(old, "111111111111", "us-east-1", cutoff, 0, map[string]ebsSnapshotCharge{
		"snap-old": {IncrementalGiB: 100, MonthlyUSD: 5},
	})
	if !ok {
		t.Fatal("expected old snapshot to match")
	}
	if rec.EstimatedMonthlyCostUSD != 5 {
		t.Fatalf("cost = %v", rec.EstimatedMonthlyCostUSD)
	}

	noCharge, ok := ebsSnapshotRecord(old, "111111111111", "us-east-1", cutoff, 0, nil)
	if !ok {
		t.Fatal("expected old snapshot to match")
	}
	if noCharge.EstimatedMonthlyCostUSD != 0 {
		t.Fatalf("cost without charge = %v, want 0", noCharge.EstimatedMonthlyCostUSD)
	}

	newSnap := old
	newSnap.StartTime = aws.Time(time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC))
	if _, ok := ebsSnapshotRecord(newSnap, "111111111111", "us-east-1", cutoff, 0, nil); ok {
		t.Fatal("expected new snapshot to be filtered out")
	}

	small := old
	small.VolumeSize = aws.Int32(1)
	if _, ok := ebsSnapshotRecord(small, "111111111111", "us-east-1", cutoff, 10, nil); ok {
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
	records, _, err := listEBSSnapshotsWithClient(context.Background(), client, "111111111111", "us-east-1", cutoff, 0)
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
	dbInstances      []rdstypes.DBInstance
	dbClusters       []rdstypes.DBCluster
	dbSnapshots      []rdstypes.DBSnapshot
	clusterSnapshots []rdstypes.DBClusterSnapshot
}

func (f *fakeRDS) DescribeDBInstances(
	_ context.Context,
	_ *rds.DescribeDBInstancesInput,
	_ ...func(*rds.Options),
) (*rds.DescribeDBInstancesOutput, error) {
	return &rds.DescribeDBInstancesOutput{DBInstances: f.dbInstances}, nil
}

func (f *fakeRDS) DescribeDBClusters(
	_ context.Context,
	_ *rds.DescribeDBClustersInput,
	_ ...func(*rds.Options),
) (*rds.DescribeDBClustersOutput, error) {
	return &rds.DescribeDBClustersOutput{DBClusters: f.dbClusters}, nil
}

func (f *fakeRDS) DescribeDBSnapshots(
	_ context.Context,
	_ *rds.DescribeDBSnapshotsInput,
	_ ...func(*rds.Options),
) (*rds.DescribeDBSnapshotsOutput, error) {
	return &rds.DescribeDBSnapshotsOutput{DBSnapshots: f.dbSnapshots}, nil
}

func TestRDSMonthlyBackupRunRateUSD(t *testing.T) {
	got := RDSMonthlyBackupRunRateUSD(1425)
	want := 1425 * 0.095
	if got != want {
		t.Fatalf("run rate = %v, want %v", got, want)
	}
	if RDSMonthlyBackupRunRateUSD(0) != 0 {
		t.Fatal("expected zero run rate for zero excess")
	}
}

func TestApplyRDSRegionalCosts(t *testing.T) {
	records := []Record{
		{Kind: KindRDSSnapshot, SizeGiB: 100, SnapshotType: "manual"},
		{Kind: KindRDSSnapshot, SizeGiB: 100, SnapshotType: "manual"},
		{Kind: KindEBSSnapshot, SizeGiB: 50, EstimatedMonthlyCostUSD: 2.5},
	}
	ApplyRDSRegionalCosts(records, RDSRegionContext{FreePoolGiB: 150, TotalBackupGiB: 300, BillableBackupGiB: 200})
	if records[0].EstimatedMonthlyCostUSD != 7.125 {
		t.Fatalf("snapshot 0 cost = %v, want 7.125", records[0].EstimatedMonthlyCostUSD)
	}
	if records[1].EstimatedMonthlyCostUSD != 7.125 {
		t.Fatalf("snapshot 1 cost = %v, want 7.125", records[1].EstimatedMonthlyCostUSD)
	}
	if records[0].CostBasis != CostBasisRDSRegionalExcess {
		t.Fatalf("cost basis = %q", records[0].CostBasis)
	}
	if records[2].EstimatedMonthlyCostUSD != 2.5 {
		t.Fatalf("ebs cost changed = %v", records[2].EstimatedMonthlyCostUSD)
	}

	withinFree := []Record{{Kind: KindRDSSnapshot, SizeGiB: 100, SnapshotType: "manual"}}
	ApplyRDSRegionalCosts(withinFree, RDSRegionContext{FreePoolGiB: 200, TotalBackupGiB: 100})
	if withinFree[0].EstimatedMonthlyCostUSD != 0 {
		t.Fatalf("within free tier cost = %v, want 0", withinFree[0].EstimatedMonthlyCostUSD)
	}
	if withinFree[0].CostBasis != CostBasisRDSWithinFreeTier {
		t.Fatalf("cost basis = %q", withinFree[0].CostBasis)
	}

	automatedLive := []Record{{
		Kind:             KindRDSSnapshot,
		SizeGiB:          100,
		SnapshotType:     "automated",
		SourceResourceID: "db-live",
	}}
	ApplyRDSRegionalCosts(automatedLive, RDSRegionContext{
		FreePoolGiB:       50,
		TotalBackupGiB:    500,
		BillableBackupGiB: 500,
		LiveInstanceIDs:   map[string]struct{}{"db-live": {}},
	})
	if automatedLive[0].EstimatedMonthlyCostUSD != 0 {
		t.Fatalf("automated live source cost = %v, want 0", automatedLive[0].EstimatedMonthlyCostUSD)
	}

	// Automated live backups can inflate regional excess; only billable GiB is removable.
	majorityAutomatedLive := []Record{{
		Kind:         KindRDSSnapshot,
		SizeGiB:      50,
		SnapshotType: "manual",
	}}
	ApplyRDSRegionalCosts(majorityAutomatedLive, RDSRegionContext{
		FreePoolGiB:       100,
		TotalBackupGiB:    250,
		BillableBackupGiB: 50,
	})
	wantMajority := RDSMonthlyBackupRunRateUSD(50)
	if majorityAutomatedLive[0].EstimatedMonthlyCostUSD != wantMajority {
		t.Fatalf("manual snapshot cost = %v, want %v (billable excess cap)", majorityAutomatedLive[0].EstimatedMonthlyCostUSD, wantMajority)
	}
}

func TestApplyRDSRegionalCostsSubsetUsesRegionalBillableGiB(t *testing.T) {
	// Listed snapshots are a subset of regional billable backup; cost share uses all billable GiB.
	records := []Record{
		{Kind: KindRDSSnapshot, SizeGiB: 100, SnapshotType: "manual"},
	}
	ApplyRDSRegionalCosts(records, RDSRegionContext{
		FreePoolGiB:       0,
		TotalBackupGiB:    500,
		BillableBackupGiB: 500,
	})
	want := RDSMonthlyBackupRunRateUSD(500) * (100.0 / 500.0)
	if records[0].EstimatedMonthlyCostUSD != want {
		t.Fatalf("subset snapshot cost = %v, want %v", records[0].EstimatedMonthlyCostUSD, want)
	}
}

func TestGetRDSRegionContextWithClient(t *testing.T) {
	client := &fakeRDS{
		dbInstances: []rdstypes.DBInstance{
			{DBInstanceIdentifier: aws.String("db-1"), AllocatedStorage: aws.Int32(100)},
		},
		dbClusters: []rdstypes.DBCluster{
			{DBClusterIdentifier: aws.String("cluster-1"), AllocatedStorage: aws.Int32(50)},
		},
		dbSnapshots: []rdstypes.DBSnapshot{
			{
				DBInstanceIdentifier: aws.String("db-1"),
				AllocatedStorage:     aws.Int32(80),
				SnapshotType:         aws.String("automated"),
			},
			{
				DBInstanceIdentifier: aws.String("db-1"),
				AllocatedStorage:     aws.Int32(20),
				SnapshotType:         aws.String("automated"),
			},
		},
		clusterSnapshots: []rdstypes.DBClusterSnapshot{
			{
				DBClusterIdentifier: aws.String("cluster-1"),
				AllocatedStorage:    aws.Int32(30),
				SnapshotType:        aws.String("manual"),
			},
		},
	}
	ctx, err := getRDSRegionContextWithClient(context.Background(), client, "us-east-1")
	if err != nil {
		t.Fatal(err)
	}
	if ctx.FreePoolGiB != 150 {
		t.Fatalf("free pool = %v, want 150", ctx.FreePoolGiB)
	}
	if ctx.TotalBackupGiB != 110 {
		t.Fatalf("total backup = %v, want 110", ctx.TotalBackupGiB)
	}
	if ctx.BillableBackupGiB != 30 {
		t.Fatalf("billable backup = %v, want 30", ctx.BillableBackupGiB)
	}
	if _, ok := ctx.LiveInstanceIDs["db-1"]; !ok {
		t.Fatalf("live instances = %#v", ctx.LiveInstanceIDs)
	}
}

func TestProvisionedStorageSkipsClusterMembers(t *testing.T) {
	instances := []rdstypes.DBInstance{
		{
			DBInstanceIdentifier: aws.String("aurora-1"),
			DBClusterIdentifier:  aws.String("cluster-1"),
			AllocatedStorage:     aws.Int32(100),
		},
	}
	clusters := []rdstypes.DBCluster{
		{DBClusterIdentifier: aws.String("cluster-1"), AllocatedStorage: aws.Int32(200)},
	}
	if got := provisionedStorageGiB(instances, clusters); got != 200 {
		t.Fatalf("provisioned storage = %v, want 200", got)
	}
}

func TestEstimateBackupStorageGiBUsesPerSourceIncrementalModel(t *testing.T) {
	snapshots := []rdstypes.DBSnapshot{
		{
			DBInstanceIdentifier: aws.String("db-1"),
			AllocatedStorage:     aws.Int32(100),
			SnapshotType:         aws.String("automated"),
		},
		{
			DBInstanceIdentifier: aws.String("db-1"),
			AllocatedStorage:     aws.Int32(100),
			SnapshotType:         aws.String("automated"),
		},
		{
			DBInstanceIdentifier: aws.String("db-1"),
			AllocatedStorage:     aws.Int32(100),
			SnapshotType:         aws.String("manual"),
		},
	}
	if got := estimateBackupStorageGiB(snapshots, nil); got != 200 {
		t.Fatalf("backup estimate = %v, want 200", got)
	}
}

func (f *fakeRDS) DescribeDBClusterSnapshots(
	_ context.Context,
	_ *rds.DescribeDBClusterSnapshotsInput,
	_ ...func(*rds.Options),
) (*rds.DescribeDBClusterSnapshotsOutput, error) {
	return &rds.DescribeDBClusterSnapshotsOutput{DBClusterSnapshots: f.clusterSnapshots}, nil
}
