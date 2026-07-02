package apilog

import (
	"context"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
)

type ec2API interface {
	DescribeRegions(context.Context, *ec2.DescribeRegionsInput, ...func(*ec2.Options)) (*ec2.DescribeRegionsOutput, error)
	DescribeSnapshots(context.Context, *ec2.DescribeSnapshotsInput, ...func(*ec2.Options)) (*ec2.DescribeSnapshotsOutput, error)
}

type loggingEC2 struct {
	inner ec2API
}

// WrapEC2 returns a client that logs EC2 snapshot-scan API calls when ctx has an apilog logger.
func WrapEC2(inner ec2API) ec2API {
	if inner == nil {
		return nil
	}
	return loggingEC2{inner: inner}
}

func (l loggingEC2) DescribeRegions(
	ctx context.Context,
	params *ec2.DescribeRegionsInput,
	optFns ...func(*ec2.Options),
) (*ec2.DescribeRegionsOutput, error) {
	LogDescribeRegions(ctx, params)
	return l.inner.DescribeRegions(ctx, params, optFns...)
}

func (l loggingEC2) DescribeSnapshots(
	ctx context.Context,
	params *ec2.DescribeSnapshotsInput,
	optFns ...func(*ec2.Options),
) (*ec2.DescribeSnapshotsOutput, error) {
	LogDescribeSnapshots(ctx, params)
	return l.inner.DescribeSnapshots(ctx, params, optFns...)
}

// LogDescribeRegions logs an EC2 DescribeRegions request when verbose mode is enabled.
func LogDescribeRegions(ctx context.Context, in *ec2.DescribeRegionsInput) {
	if in == nil {
		return
	}
	Log(ctx, formatDescribeRegions(in))
}

// LogDescribeSnapshots logs an EC2 DescribeSnapshots request when verbose mode is enabled.
func LogDescribeSnapshots(ctx context.Context, in *ec2.DescribeSnapshotsInput) {
	if in == nil {
		return
	}
	Log(ctx, formatDescribeSnapshots(in))
}

func formatDescribeRegions(in *ec2.DescribeRegionsInput) string {
	var b strings.Builder
	b.WriteString("EC2.DescribeRegions")
	if in.AllRegions != nil {
		fmt.Fprintf(&b, " allRegions=%t", aws.ToBool(in.AllRegions))
	}
	return b.String()
}

func formatDescribeSnapshots(in *ec2.DescribeSnapshotsInput) string {
	var b strings.Builder
	b.WriteString("EC2.DescribeSnapshots")
	if len(in.OwnerIds) > 0 {
		fmt.Fprintf(&b, " owners=%s", strings.Join(in.OwnerIds, ","))
	}
	if in.NextToken != nil && aws.ToString(in.NextToken) != "" {
		b.WriteString(" page=next")
	}
	return b.String()
}
