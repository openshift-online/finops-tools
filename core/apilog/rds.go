package apilog

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/rds"
)

type rdsAPI interface {
	DescribeDBInstances(context.Context, *rds.DescribeDBInstancesInput, ...func(*rds.Options)) (*rds.DescribeDBInstancesOutput, error)
	DescribeDBClusters(context.Context, *rds.DescribeDBClustersInput, ...func(*rds.Options)) (*rds.DescribeDBClustersOutput, error)
	DescribeDBSnapshots(context.Context, *rds.DescribeDBSnapshotsInput, ...func(*rds.Options)) (*rds.DescribeDBSnapshotsOutput, error)
	DescribeDBClusterSnapshots(context.Context, *rds.DescribeDBClusterSnapshotsInput, ...func(*rds.Options)) (*rds.DescribeDBClusterSnapshotsOutput, error)
}

type loggingRDS struct {
	inner rdsAPI
}

// WrapRDS returns a client that logs RDS snapshot-scan API calls when ctx has an apilog logger.
func WrapRDS(inner rdsAPI) rdsAPI {
	if inner == nil {
		return nil
	}
	return loggingRDS{inner: inner}
}

func (l loggingRDS) DescribeDBInstances(
	ctx context.Context,
	params *rds.DescribeDBInstancesInput,
	optFns ...func(*rds.Options),
) (*rds.DescribeDBInstancesOutput, error) {
	Log(ctx, formatRDSCall("RDS.DescribeDBInstances", params.Marker))
	return l.inner.DescribeDBInstances(ctx, params, optFns...)
}

func (l loggingRDS) DescribeDBClusters(
	ctx context.Context,
	params *rds.DescribeDBClustersInput,
	optFns ...func(*rds.Options),
) (*rds.DescribeDBClustersOutput, error) {
	Log(ctx, formatRDSCall("RDS.DescribeDBClusters", params.Marker))
	return l.inner.DescribeDBClusters(ctx, params, optFns...)
}

func (l loggingRDS) DescribeDBSnapshots(
	ctx context.Context,
	params *rds.DescribeDBSnapshotsInput,
	optFns ...func(*rds.Options),
) (*rds.DescribeDBSnapshotsOutput, error) {
	Log(ctx, formatRDSCall("RDS.DescribeDBSnapshots", params.Marker))
	return l.inner.DescribeDBSnapshots(ctx, params, optFns...)
}

func (l loggingRDS) DescribeDBClusterSnapshots(
	ctx context.Context,
	params *rds.DescribeDBClusterSnapshotsInput,
	optFns ...func(*rds.Options),
) (*rds.DescribeDBClusterSnapshotsOutput, error) {
	Log(ctx, formatRDSCall("RDS.DescribeDBClusterSnapshots", params.Marker))
	return l.inner.DescribeDBClusterSnapshots(ctx, params, optFns...)
}

func formatRDSCall(op string, marker *string) string {
	if marker != nil && aws.ToString(marker) != "" {
		return op + " page=next"
	}
	return op
}
