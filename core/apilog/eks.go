package apilog

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/eks"
)

type eksAPI interface {
	ListClusters(context.Context, *eks.ListClustersInput, ...func(*eks.Options)) (*eks.ListClustersOutput, error)
	DescribeCluster(context.Context, *eks.DescribeClusterInput, ...func(*eks.Options)) (*eks.DescribeClusterOutput, error)
	DescribeClusterVersions(context.Context, *eks.DescribeClusterVersionsInput, ...func(*eks.Options)) (*eks.DescribeClusterVersionsOutput, error)
}

type loggingEKS struct{ inner eksAPI }

// WrapEKS logs discovery calls when the context has an API logger.
func WrapEKS(inner eksAPI) eksAPI {
	if inner == nil {
		return nil
	}
	return loggingEKS{inner: inner}
}

func (l loggingEKS) ListClusters(ctx context.Context, input *eks.ListClustersInput, opts ...func(*eks.Options)) (*eks.ListClustersOutput, error) {
	Log(ctx, formatEKSCall("EKS.ListClusters", input.NextToken))
	return l.inner.ListClusters(ctx, input, opts...)
}

func (l loggingEKS) DescribeCluster(ctx context.Context, input *eks.DescribeClusterInput, opts ...func(*eks.Options)) (*eks.DescribeClusterOutput, error) {
	Log(ctx, fmt.Sprintf("EKS.DescribeCluster name=%s", aws.ToString(input.Name)))
	return l.inner.DescribeCluster(ctx, input, opts...)
}

func (l loggingEKS) DescribeClusterVersions(ctx context.Context, input *eks.DescribeClusterVersionsInput, opts ...func(*eks.Options)) (*eks.DescribeClusterVersionsOutput, error) {
	Log(ctx, formatEKSCall("EKS.DescribeClusterVersions", input.NextToken))
	return l.inner.DescribeClusterVersions(ctx, input, opts...)
}

func formatEKSCall(operation string, token *string) string {
	if aws.ToString(token) != "" {
		return operation + " page=next"
	}
	return operation
}
