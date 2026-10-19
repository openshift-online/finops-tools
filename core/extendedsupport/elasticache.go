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
	catalog   *HealthLifecycleCatalog
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
		err := scanElastiCachePages(ctx, client, source.page, accountID, region, now, l.catalog, &result)
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

func scanElastiCachePages(ctx context.Context, client ElastiCacheAPI, page elastiCachePage, accountID, region string, now time.Time, catalog *HealthLifecycleCatalog, result *regionScanResult) error {
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
			record, warning := classifyElastiCache(resource, accountID, region, now, catalog)
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

func classifyElastiCache(cache elastiCacheResource, accountID, region string, now time.Time, catalog *HealthLifecycleCatalog) (*Record, *ResourceWarning) {
	engine, version := strings.ToLower(strings.TrimSpace(cache.engine)), strings.TrimSpace(cache.version)
	warning := &ResourceWarning{
		AccountID: accountID, Region: region, Service: ServiceElastiCache,
		ResourceType: cache.resourceType, ResourceID: cache.id, Engine: engine, EngineVersion: version,
	}
	switch {
	case cache.id == "":
		warning.Message = "ElastiCache cache identifier is missing"
	case engine == "":
		warning.Message = "ElastiCache engine is missing"
	case engine != "redis" && engine != "valkey" && engine != "memcached":
		warning.Message = "ElastiCache lifecycle information for engine " + engine + " is missing"
	case version == "":
		warning.Message = "ElastiCache engine version is missing"
	default:
		parts := strings.Split(version, ".")
		for _, part := range parts {
			if _, err := strconv.ParseUint(part, 10, 32); err != nil {
				warning.Message = "ElastiCache engine version is invalid: " + version
				return nil, warning
			}
		}
		return classifyHealthSupport(catalog, *warning, now)
	}
	return nil, warning
}
