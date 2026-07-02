package snapshot

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ectypes "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/openshift-online/finops-tools/core/apilog"
)

// EC2API is the subset of EC2 used for snapshot discovery.
type EC2API interface {
	DescribeRegions(
		ctx context.Context,
		params *ec2.DescribeRegionsInput,
		optFns ...func(*ec2.Options),
	) (*ec2.DescribeRegionsOutput, error)
	DescribeSnapshots(
		ctx context.Context,
		params *ec2.DescribeSnapshotsInput,
		optFns ...func(*ec2.Options),
	) (*ec2.DescribeSnapshotsOutput, error)
}

type ec2ClientFactory func(aws.Config) EC2API

func newEC2Client(cfg aws.Config) EC2API {
	return apilog.WrapEC2(ec2.NewFromConfig(cfg))
}

type ebsLister interface {
	ListEBSSnapshots(
		ctx context.Context,
		cfg aws.Config,
		region, accountID string,
		cutoff time.Time,
		minSizeGiB float64,
	) ([]Record, float64, error)
}

type ec2EBSLister struct{}

func (ec2EBSLister) ListEBSSnapshots(
	ctx context.Context,
	cfg aws.Config,
	region, accountID string,
	cutoff time.Time,
	minSizeGiB float64,
) ([]Record, float64, error) {
	regionalCfg := cfg.Copy()
	regionalCfg.Region = region
	return listEBSSnapshotsWithClient(ctx, newEC2Client(regionalCfg), accountID, region, cutoff, minSizeGiB)
}

func listEBSSnapshotsWithClient(
	ctx context.Context,
	client EC2API,
	accountID, region string,
	cutoff time.Time,
	minSizeGiB float64,
) ([]Record, float64, error) {
	var allSnapshots []ectypes.Snapshot
	var token *string
	for {
		out, err := client.DescribeSnapshots(ctx, &ec2.DescribeSnapshotsInput{
			OwnerIds:  []string{"self"},
			NextToken: token,
		})
		if err != nil {
			return nil, 0, fmt.Errorf("describe ebs snapshots in %s: %w", region, err)
		}
		allSnapshots = append(allSnapshots, out.Snapshots...)
		if out.NextToken == nil || aws.ToString(out.NextToken) == "" {
			break
		}
		token = out.NextToken
	}

	charges := computeEBSSnapshotCharges(allSnapshots)
	var records []Record
	for _, snap := range allSnapshots {
		rec, ok := ebsSnapshotRecord(snap, accountID, region, cutoff, minSizeGiB, charges)
		if ok {
			records = append(records, rec)
		}
	}
	return records, ebsRegionalMonthlyCostUSD(charges), nil
}

func ebsSnapshotRecord(
	snap ectypes.Snapshot,
	accountID, region string,
	cutoff time.Time,
	minSizeGiB float64,
	charges map[string]ebsSnapshotCharge,
) (Record, bool) {
	start := aws.ToTime(snap.StartTime)
	if start.IsZero() || !start.Before(cutoff) {
		return Record{}, false
	}
	sizeGiB := float64(aws.ToInt32(snap.VolumeSize))
	if sizeGiB < minSizeGiB {
		return Record{}, false
	}
	tier := strings.TrimSpace(string(snap.StorageTier))
	rec := Record{
		AccountID:        accountID,
		Region:           region,
		Kind:             KindEBSSnapshot,
		ResourceID:       aws.ToString(snap.SnapshotId),
		SourceResourceID: aws.ToString(snap.VolumeId),
		CreatedAt:        start.UTC(),
		AgeDays:          ageDays(start),
		SizeGiB:          sizeGiB,
		StorageTier:      tier,
		Description:      aws.ToString(snap.Description),
		Tags:             tagMapFromEC2(snap.Tags),
	}
	charge, ok := charges[rec.ResourceID]
	applyEBSChargeToRecord(&rec, charge, ok)
	return rec, true
}

func tagMapFromEC2(tags []ectypes.Tag) map[string]string {
	if len(tags) == 0 {
		return nil
	}
	out := make(map[string]string, len(tags))
	for _, tag := range tags {
		key := strings.TrimSpace(aws.ToString(tag.Key))
		if key == "" {
			continue
		}
		out[key] = aws.ToString(tag.Value)
	}
	return out
}

func ageDays(created time.Time) int {
	if created.IsZero() {
		return 0
	}
	now := time.Now().UTC()
	d := now.Sub(created.UTC())
	if d < 0 {
		return 0
	}
	return int(d / (24 * time.Hour))
}
