package apilog

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/elasticache"
)

type elastiCacheAPI interface {
	DescribeCacheClusters(context.Context, *elasticache.DescribeCacheClustersInput, ...func(*elasticache.Options)) (*elasticache.DescribeCacheClustersOutput, error)
	DescribeServerlessCaches(context.Context, *elasticache.DescribeServerlessCachesInput, ...func(*elasticache.Options)) (*elasticache.DescribeServerlessCachesOutput, error)
	ListTagsForResource(context.Context, *elasticache.ListTagsForResourceInput, ...func(*elasticache.Options)) (*elasticache.ListTagsForResourceOutput, error)
}

type loggingElastiCache struct{ inner elastiCacheAPI }

// WrapElastiCache logs discovery calls when the context has an API logger.
func WrapElastiCache(inner elastiCacheAPI) elastiCacheAPI {
	if inner == nil {
		return nil
	}
	return loggingElastiCache{inner: inner}
}

func (l loggingElastiCache) DescribeCacheClusters(ctx context.Context, input *elasticache.DescribeCacheClustersInput, opts ...func(*elasticache.Options)) (*elasticache.DescribeCacheClustersOutput, error) {
	Log(ctx, formatElastiCacheCall("ElastiCache.DescribeCacheClusters", input.Marker))
	return l.inner.DescribeCacheClusters(ctx, input, opts...)
}

func (l loggingElastiCache) DescribeServerlessCaches(ctx context.Context, input *elasticache.DescribeServerlessCachesInput, opts ...func(*elasticache.Options)) (*elasticache.DescribeServerlessCachesOutput, error) {
	Log(ctx, formatElastiCacheCall("ElastiCache.DescribeServerlessCaches", input.NextToken))
	return l.inner.DescribeServerlessCaches(ctx, input, opts...)
}

func (l loggingElastiCache) ListTagsForResource(ctx context.Context, input *elasticache.ListTagsForResourceInput, opts ...func(*elasticache.Options)) (*elasticache.ListTagsForResourceOutput, error) {
	Log(ctx, fmt.Sprintf("ElastiCache.ListTagsForResource resource=%s", aws.ToString(input.ResourceName)))
	return l.inner.ListTagsForResource(ctx, input, opts...)
}

func formatElastiCacheCall(operation string, token *string) string {
	if aws.ToString(token) != "" {
		return operation + " page=next"
	}
	return operation
}
