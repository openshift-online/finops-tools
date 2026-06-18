package snapshot

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/rds"
	rdstypes "github.com/aws/aws-sdk-go-v2/service/rds/types"
)

// RDSAPI is the subset of RDS used for snapshot discovery.
type RDSAPI interface {
	DescribeDBSnapshots(
		ctx context.Context,
		params *rds.DescribeDBSnapshotsInput,
		optFns ...func(*rds.Options),
	) (*rds.DescribeDBSnapshotsOutput, error)
	DescribeDBClusterSnapshots(
		ctx context.Context,
		params *rds.DescribeDBClusterSnapshotsInput,
		optFns ...func(*rds.Options),
	) (*rds.DescribeDBClusterSnapshotsOutput, error)
}

type rdsClientFactory func(aws.Config) RDSAPI

func newRDSClient(cfg aws.Config) RDSAPI {
	return rds.NewFromConfig(cfg)
}

type rdsLister interface {
	ListRDSSnapshots(
		ctx context.Context,
		cfg aws.Config,
		region, accountID string,
		cutoff time.Time,
		minSizeGiB float64,
	) ([]Record, error)
}

type awsRDSLister struct{}

func (awsRDSLister) ListRDSSnapshots(
	ctx context.Context,
	cfg aws.Config,
	region, accountID string,
	cutoff time.Time,
	minSizeGiB float64,
) ([]Record, error) {
	regionalCfg := cfg.Copy()
	regionalCfg.Region = region
	client := newRDSClient(regionalCfg)
	instanceRecords, err := listRDSSnapshotsWithClient(ctx, client, accountID, region, cutoff, minSizeGiB)
	if err != nil {
		return nil, err
	}
	clusterRecords, err := listRDSClusterSnapshotsWithClient(ctx, client, accountID, region, cutoff, minSizeGiB)
	if err != nil {
		return nil, err
	}
	return append(instanceRecords, clusterRecords...), nil
}

func listRDSSnapshotsWithClient(
	ctx context.Context,
	client RDSAPI,
	accountID, region string,
	cutoff time.Time,
	minSizeGiB float64,
) ([]Record, error) {
	var records []Record
	var token *string
	for {
		out, err := client.DescribeDBSnapshots(ctx, &rds.DescribeDBSnapshotsInput{
			Marker: token,
		})
		if err != nil {
			if isRDSRegionUnsupported(err) {
				return nil, nil
			}
			return nil, fmt.Errorf("describe rds snapshots in %s: %w", region, err)
		}
		for _, snap := range out.DBSnapshots {
			rec, ok := rdsSnapshotRecord(snap, accountID, region, cutoff, minSizeGiB)
			if ok {
				records = append(records, rec)
			}
		}
		if out.Marker == nil || aws.ToString(out.Marker) == "" {
			break
		}
		token = out.Marker
	}
	return records, nil
}

func listRDSClusterSnapshotsWithClient(
	ctx context.Context,
	client RDSAPI,
	accountID, region string,
	cutoff time.Time,
	minSizeGiB float64,
) ([]Record, error) {
	var records []Record
	var token *string
	for {
		out, err := client.DescribeDBClusterSnapshots(ctx, &rds.DescribeDBClusterSnapshotsInput{
			Marker: token,
		})
		if err != nil {
			if isRDSRegionUnsupported(err) {
				return nil, nil
			}
			return nil, fmt.Errorf("describe rds cluster snapshots in %s: %w", region, err)
		}
		for _, snap := range out.DBClusterSnapshots {
			rec, ok := rdsClusterSnapshotRecord(snap, accountID, region, cutoff, minSizeGiB)
			if ok {
				records = append(records, rec)
			}
		}
		if out.Marker == nil || aws.ToString(out.Marker) == "" {
			break
		}
		token = out.Marker
	}
	return records, nil
}

func rdsSnapshotRecord(
	snap rdstypes.DBSnapshot,
	accountID, region string,
	cutoff time.Time,
	minSizeGiB float64,
) (Record, bool) {
	created := aws.ToTime(snap.SnapshotCreateTime)
	if created.IsZero() || !created.Before(cutoff) {
		return Record{}, false
	}
	sizeGiB := float64(aws.ToInt32(snap.AllocatedStorage))
	if sizeGiB < minSizeGiB {
		return Record{}, false
	}
	return Record{
		AccountID:        accountID,
		Region:           region,
		Kind:             KindRDSSnapshot,
		ResourceID:       aws.ToString(snap.DBSnapshotIdentifier),
		SourceResourceID: aws.ToString(snap.DBInstanceIdentifier),
		CreatedAt:        created.UTC(),
		AgeDays:          ageDays(created),
		SizeGiB:          sizeGiB,
		SnapshotType:     strings.TrimSpace(aws.ToString(snap.SnapshotType)),
	}, true
}

func rdsClusterSnapshotRecord(
	snap rdstypes.DBClusterSnapshot,
	accountID, region string,
	cutoff time.Time,
	minSizeGiB float64,
) (Record, bool) {
	created := aws.ToTime(snap.SnapshotCreateTime)
	if created.IsZero() || !created.Before(cutoff) {
		return Record{}, false
	}
	sizeGiB := float64(aws.ToInt32(snap.AllocatedStorage))
	if sizeGiB < minSizeGiB {
		return Record{}, false
	}
	return Record{
		AccountID:        accountID,
		Region:           region,
		Kind:             KindRDSClusterSnapshot,
		ResourceID:       aws.ToString(snap.DBClusterSnapshotIdentifier),
		SourceResourceID: aws.ToString(snap.DBClusterIdentifier),
		CreatedAt:        created.UTC(),
		AgeDays:          ageDays(created),
		SizeGiB:          sizeGiB,
		SnapshotType:     strings.TrimSpace(aws.ToString(snap.SnapshotType)),
	}, true
}

func isRDSRegionUnsupported(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "not available in this region")
}
