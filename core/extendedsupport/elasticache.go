package extendedsupport

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/elasticache"
	"github.com/openshift-online/finops-tools/core/apilog"
)

// ElastiCacheAPI is the subset used for cache discovery and resource tags.
type ElastiCacheAPI interface {
	DescribeCacheClusters(context.Context, *elasticache.DescribeCacheClustersInput, ...func(*elasticache.Options)) (*elasticache.DescribeCacheClustersOutput, error)
	DescribeServerlessCaches(context.Context, *elasticache.DescribeServerlessCachesInput, ...func(*elasticache.Options)) (*elasticache.DescribeServerlessCachesOutput, error)
	ListTagsForResource(context.Context, *elasticache.ListTagsForResourceInput, ...func(*elasticache.Options)) (*elasticache.ListTagsForResourceOutput, error)
}

type elastiCacheLister interface {
	ListElastiCacheExtendedSupport(context.Context, aws.Config, string, string, time.Time) (regionScanResult, error)
}

type awsElastiCacheLister struct {
	newClient func(aws.Config) ElastiCacheAPI
}

type elastiCacheResource struct {
	id, arn, engine, version string
	resourceType             ResourceType
}

type elastiCachePage func(context.Context, *string) ([]elastiCacheResource, *string, error)

func (l awsElastiCacheLister) ListElastiCacheExtendedSupport(ctx context.Context, cfg aws.Config, region, accountID string, now time.Time) (regionScanResult, error) {
	regionalCfg := cfg.Copy()
	regionalCfg.Region = region
	var client ElastiCacheAPI
	if l.newClient != nil {
		client = l.newClient(regionalCfg)
	} else {
		client = apilog.WrapElastiCache(elasticache.NewFromConfig(regionalCfg))
	}
	var result regionScanResult
	// DescribeCacheClusters includes replication-group members when the standalone
	// filter is unset. Report each cache cluster once, without counting its parent
	// replication group again; members can run different versions during upgrades.
	sources := []struct {
		operation string
		page      elastiCachePage
	}{
		{"ElastiCache.DescribeCacheClusters", func(ctx context.Context, marker *string) ([]elastiCacheResource, *string, error) {
			out, err := client.DescribeCacheClusters(ctx, &elasticache.DescribeCacheClustersInput{Marker: marker, MaxRecords: aws.Int32(100)})
			if err != nil {
				return nil, nil, err
			}
			if out == nil {
				return nil, nil, fmt.Errorf("returned no response")
			}
			resources := make([]elastiCacheResource, 0, len(out.CacheClusters))
			for _, cache := range out.CacheClusters {
				resources = append(resources, elastiCacheResource{
					id: aws.ToString(cache.CacheClusterId), arn: aws.ToString(cache.ARN),
					engine: aws.ToString(cache.Engine), version: aws.ToString(cache.EngineVersion),
					resourceType: ResourceTypeElastiCacheCluster,
				})
			}
			return resources, out.Marker, nil
		}},
		{"ElastiCache.DescribeServerlessCaches", func(ctx context.Context, token *string) ([]elastiCacheResource, *string, error) {
			out, err := client.DescribeServerlessCaches(ctx, &elasticache.DescribeServerlessCachesInput{NextToken: token, MaxResults: aws.Int32(100)})
			if err != nil {
				return nil, nil, err
			}
			if out == nil {
				return nil, nil, fmt.Errorf("returned no response")
			}
			resources := make([]elastiCacheResource, 0, len(out.ServerlessCaches))
			for _, cache := range out.ServerlessCaches {
				version := strings.TrimSpace(aws.ToString(cache.FullEngineVersion))
				if version == "" {
					version = aws.ToString(cache.MajorEngineVersion)
				}
				resources = append(resources, elastiCacheResource{
					id: aws.ToString(cache.ServerlessCacheName), arn: aws.ToString(cache.ARN),
					engine: aws.ToString(cache.Engine), version: version,
					resourceType: ResourceTypeElastiCacheServerless,
				})
			}
			return resources, out.NextToken, nil
		}},
	}
	for _, source := range sources {
		err := scanElastiCachePages(ctx, client, source.page, accountID, region, now, &result)
		if errors.Is(err, context.Canceled) || ctx.Err() != nil {
			if ctx.Err() != nil {
				return result, ctx.Err()
			}
			return result, err
		}
		if err != nil {
			result.Warnings = append(result.Warnings, RegionWarning{
				AccountID: accountID, Region: region, Service: ServiceElastiCache,
				Message: source.operation + " discovery incomplete: " + warningMessage(err),
			})
		}
	}
	return result, nil
}

func scanElastiCachePages(ctx context.Context, client ElastiCacheAPI, page elastiCachePage, accountID, region string, now time.Time, result *regionScanResult) error {
	seenIDs, seenTokens := make(map[string]bool), make(map[string]bool)
	var token *string
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		resources, next, err := page(ctx, token)
		if err != nil {
			return err
		}
		for _, resource := range resources {
			if err := ctx.Err(); err != nil {
				return err
			}
			resource.id = strings.TrimSpace(resource.id)
			if resource.id != "" && seenIDs[resource.id] {
				continue
			}
			seenIDs[resource.id] = true
			record, warning := classifyElastiCache(resource, accountID, region, now)
			if warning != nil {
				result.UnclassifiedResources = append(result.UnclassifiedResources, *warning)
			}
			if record == nil {
				continue
			}
			// Missing tags do not change the support classification or discard a cache.
			var tagErr error
			if strings.TrimSpace(resource.arn) == "" {
				tagErr = fmt.Errorf("cache ARN is missing")
			} else {
				out, err := client.ListTagsForResource(ctx, &elasticache.ListTagsForResourceInput{ResourceName: aws.String(resource.arn)})
				tagErr = err
				if err == nil && out == nil {
					tagErr = fmt.Errorf("returned no response")
				}
				if err == nil && out != nil {
					record.Tags = make(map[string]string, len(out.TagList))
					for _, tag := range out.TagList {
						if key := aws.ToString(tag.Key); key != "" {
							record.Tags[key] = aws.ToString(tag.Value)
						}
					}
				}
			}
			if errors.Is(tagErr, context.Canceled) || ctx.Err() != nil {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				return tagErr
			}
			if tagErr != nil {
				result.Warnings = append(result.Warnings, RegionWarning{
					AccountID: accountID, Region: region, Service: ServiceElastiCache,
					Message: fmt.Sprintf("ElastiCache.ListTagsForResource for %s: %s (resource retained without tags)", resource.id, warningMessage(tagErr)),
				})
			}
			result.Records = append(result.Records, *record)
		}
		if aws.ToString(next) == "" {
			return nil
		}
		if seenTokens[aws.ToString(next)] {
			return fmt.Errorf("returned a repeated pagination token")
		}
		seenTokens[aws.ToString(next)] = true
		token = next
	}
}

// ElastiCache's DescribeCacheEngineVersions API has no lifecycle dates. This
// schedule is maintained from AWS's published Redis OSS support calendar:
// https://docs.aws.amazon.com/AmazonElastiCache/latest/dg/extended-support-versions.html
// https://docs.aws.amazon.com/AmazonElastiCache/latest/dg/engine-versions.html
// Reviewed 2026-10-19. Dates are inclusive calendar dates in UTC. Update this
// table when AWS announces new dates; unknown versions produce warnings.
type elastiCacheLifecycle struct{ standardEnd, extendedEnd time.Time }

var elastiCacheRedisLifecycle = map[string]elastiCacheLifecycle{
	"4": {standardEnd: time.Date(2026, 1, 31, 0, 0, 0, 0, time.UTC), extendedEnd: time.Date(2029, 1, 31, 0, 0, 0, 0, time.UTC)},
	"5": {standardEnd: time.Date(2026, 1, 31, 0, 0, 0, 0, time.UTC), extendedEnd: time.Date(2029, 1, 31, 0, 0, 0, 0, time.UTC)},
	"6": {standardEnd: time.Date(2027, 1, 31, 0, 0, 0, 0, time.UTC), extendedEnd: time.Date(2030, 1, 31, 0, 0, 0, 0, time.UTC)},
}

func classifyElastiCache(cache elastiCacheResource, accountID, region string, now time.Time) (*Record, *ResourceWarning) {
	engine, version := strings.ToLower(strings.TrimSpace(cache.engine)), strings.TrimSpace(cache.version)
	// AWS has no announced paid Extended Support for Valkey or Memcached.
	if engine == "valkey" || engine == "memcached" {
		return nil, nil
	}
	warning := &ResourceWarning{
		AccountID: accountID, Region: region, Service: ServiceElastiCache,
		ResourceType: cache.resourceType, ResourceID: cache.id, Engine: engine, EngineVersion: version,
	}
	switch {
	case cache.id == "":
		warning.Message = "ElastiCache cache identifier is missing"
	case engine == "":
		warning.Message = "ElastiCache engine is missing"
	case engine != "redis":
		warning.Message = "ElastiCache lifecycle information for engine " + engine + " is missing"
	case version == "":
		warning.Message = "ElastiCache engine version is missing"
	default:
		parts := strings.Split(version, ".")
		for _, part := range parts {
			if _, err := strconv.ParseUint(part, 10, 32); err != nil {
				warning.Message = "ElastiCache Redis OSS engine version is invalid: " + version
				return nil, warning
			}
		}
		major := parts[0]
		// Redis OSS 7 is supported and has no announced standard-support end.
		if major == "7" {
			return nil, nil
		}
		lifecycle, ok := elastiCacheRedisLifecycle[major]
		switch {
		case !ok:
			warning.Message = "ElastiCache lifecycle information for Redis OSS major version " + major + " is missing"
		case now.Before(lifecycle.standardEnd.AddDate(0, 0, 1)):
			return nil, nil
		case !now.Before(lifecycle.extendedEnd.AddDate(0, 0, 1)):
			warning.Message = "ElastiCache Redis OSS version is past extended support"
		default:
			return &Record{
				AccountID: accountID, Region: region, Service: ServiceElastiCache,
				ResourceType: cache.resourceType, ResourceID: cache.id, Engine: engine,
				EngineVersion: version, MajorEngineVersion: major,
				StandardSupportEndDate: lifecycle.standardEnd, ExtendedSupportEndDate: &lifecycle.extendedEnd,
			}, nil
		}
	}
	return nil, warning
}
