package snapshot

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/rds"
)

func TestIsRDSRegionUnsupported(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "nil", err: nil, want: false},
		{
			name: "rds unavailable in region",
			err:  fmt.Errorf(`operation error RDS: DescribeDBSnapshots, InvalidParameterValue: The API for service 'rds' is not available in this region`),
			want: true,
		},
		{
			name: "connectivity failure",
			err:  fmt.Errorf(`operation error RDS: DescribeDBSnapshots, request send failed, could not connect to the endpoint`),
			want: false,
		},
		{
			name: "invalid parameter unrelated to region",
			err:  fmt.Errorf(`operation error RDS: DescribeDBSnapshots, InvalidParameterValue: Invalid snapshot identifier`),
			want: false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := isRDSRegionUnsupported(tc.err); got != tc.want {
				t.Fatalf("isRDSRegionUnsupported() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestListRDSSnapshotsWithClientSurfacesConnectivityError(t *testing.T) {
	client := &rdsClientWithErr{
		err: fmt.Errorf(`could not connect to the endpoint`),
	}
	_, err := listRDSSnapshotsWithClient(
		context.Background(),
		client,
		"111111111111",
		"us-east-1",
		time.Now().Add(-24*time.Hour),
		0,
	)
	if err == nil {
		t.Fatal("expected connectivity error to be returned")
	}
}

type rdsClientWithErr struct {
	err error
}

func (c *rdsClientWithErr) DescribeDBInstances(
	_ context.Context,
	_ *rds.DescribeDBInstancesInput,
	_ ...func(*rds.Options),
) (*rds.DescribeDBInstancesOutput, error) {
	return nil, c.err
}

func (c *rdsClientWithErr) DescribeDBClusters(
	_ context.Context,
	_ *rds.DescribeDBClustersInput,
	_ ...func(*rds.Options),
) (*rds.DescribeDBClustersOutput, error) {
	return nil, c.err
}

func (c *rdsClientWithErr) DescribeDBSnapshots(
	_ context.Context,
	_ *rds.DescribeDBSnapshotsInput,
	_ ...func(*rds.Options),
) (*rds.DescribeDBSnapshotsOutput, error) {
	return nil, c.err
}

func (c *rdsClientWithErr) DescribeDBClusterSnapshots(
	_ context.Context,
	_ *rds.DescribeDBClusterSnapshotsInput,
	_ ...func(*rds.Options),
) (*rds.DescribeDBClusterSnapshotsOutput, error) {
	return nil, c.err
}
