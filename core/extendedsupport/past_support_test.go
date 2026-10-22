package extendedsupport

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/elasticache"
	cachetypes "github.com/aws/aws-sdk-go-v2/service/elasticache/types"
	"github.com/aws/aws-sdk-go-v2/service/health"
	healthtypes "github.com/aws/aws-sdk-go-v2/service/health/types"
	"github.com/aws/aws-sdk-go-v2/service/opensearch"
	ostypes "github.com/aws/aws-sdk-go-v2/service/opensearch/types"
)

func TestHealthSupportEndEventsClassifyResources(t *testing.T) {
	standard := time.Date(2032, 3, 14, 9, 26, 0, 0, time.UTC)
	extended := standard.AddDate(1, 0, 0)
	for _, withSupported := range []bool{false, true} {
		t.Run(fmt.Sprintf("supported-event=%t", withSupported), func(t *testing.T) {
			catalog := NewHealthLifecycleCatalog()
			catalogCalls := 0
			catalog.newClient = func(aws.Config) HealthAPI {
				return fakeHealthAPI(func(context.Context, *health.DescribeServiceLifecycleInput) (*health.DescribeServiceLifecycleOutput, error) {
					catalogCalls++
					events := []healthtypes.LifecycleEvent{
						healthEvent("standard_support_end", standard, "all"),
						healthEvent("extended_support_end", extended, "all"),
						healthEvent("standard_support_end", standard.AddDate(0, 2, 0), "us-west-2"),
						healthEvent("extended_support_end", extended.AddDate(0, 2, 0), "us-west-2"),
					}
					if withSupported {
						events = append(events, healthtypes.LifecycleEvent{LifecycleEventType: aws.String("supported"), Regions: []string{"all"}})
					}
					return &health.DescribeServiceLifecycleOutput{ServiceLifecycles: []healthtypes.ServiceLifecycle{
						{Service: aws.String("ELASTICACHE"), Version: aws.String("redis/5.0"), LifecycleEvents: events},
						{Service: aws.String("OPENSEARCH"), Version: aws.String("opensearch/1.2"), LifecycleEvents: events},
					}}, nil
				})
			}
			cacheClient := fakeElastiCacheAPI{
				clusters: func(context.Context, *elasticache.DescribeCacheClustersInput) (*elasticache.DescribeCacheClustersOutput, error) {
					return &elasticache.DescribeCacheClustersOutput{CacheClusters: []cachetypes.CacheCluster{testCache("cache")}}, nil
				},
				serverless: func(context.Context, *elasticache.DescribeServerlessCachesInput) (*elasticache.DescribeServerlessCachesOutput, error) {
					return &elasticache.DescribeServerlessCachesOutput{ServerlessCaches: []cachetypes.ServerlessCache{{
						ServerlessCacheName: aws.String("serverless"), ARN: aws.String("arn:aws:elasticache:us-east-1:111111111111:serverlesscache:serverless"),
						Engine: aws.String("redis"), FullEngineVersion: aws.String("5.0.6"),
					}}}, nil
				},
			}
			searchClient := fakeOpenSearchAPI{list: func(context.Context, *opensearch.ListDomainNamesInput) (*opensearch.ListDomainNamesOutput, error) {
				return &opensearch.ListDomainNamesOutput{DomainNames: []ostypes.DomainInfo{{DomainName: aws.String("domain")}}}, nil
			}}
			for _, region := range []string{"us-east-1", "us-west-2"} {
				start, end := standard, extended
				if region == "us-west-2" {
					start, end = start.AddDate(0, 2, 0), end.AddDate(0, 2, 0)
				}
				for _, tc := range []struct {
					name          string
					at            time.Time
					current, past int
				}{
					{name: "standard support", at: start.Add(-time.Nanosecond)},
					{name: "standard end boundary", at: start, current: 3},
					{name: "extended support", at: start.Add(time.Hour), current: 3},
					{name: "before extended end", at: end.Add(-time.Nanosecond), current: 3},
					{name: "extended end boundary", at: end, past: 3},
					{name: "past extended support", at: end.Add(time.Hour), past: 3},
					{name: "recheck standard support", at: start.Add(-time.Nanosecond)},
				} {
					t.Run(region+"/"+tc.name, func(t *testing.T) {
						result, err := Fetch(context.Background(), Query{
							Targets: []AccountTarget{{AccountID: "111111111111"}}, Services: []Service{ServiceElastiCache, ServiceOpenSearch}, Now: tc.at,
							HealthLifecycleCatalog: catalog, regionLister: fakeRegionLister{regions: []string{region}},
							elastiCacheLister: awsElastiCacheLister{catalog: catalog, newClient: func(aws.Config) ElastiCacheAPI { return cacheClient }},
							openSearchLister:  awsOpenSearchLister{catalog: catalog, newClient: func(aws.Config) OpenSearchAPI { return searchClient }},
						})
						if err != nil {
							t.Fatal(err)
						}
						if len(result.Records) != tc.current || result.Summary.TotalCount != tc.current || len(result.Summary.PastExtendedSupportResources) != tc.past || len(result.Summary.UnclassifiedResources)+len(result.Summary.SkippedRegions)+len(result.Summary.SkippedAccounts) != 0 {
							t.Fatalf("result=%+v", result)
						}
						for _, records := range [][]Record{result.Records, result.Summary.PastExtendedSupportResources} {
							for _, record := range records {
								if !record.StandardSupportEndDate.Equal(start) || record.ExtendedSupportEndDate == nil || !record.ExtendedSupportEndDate.Equal(end) {
									t.Fatalf("regional support dates = %+v", record)
								}
							}
						}
					})
				}
			}
			if catalogCalls != 1 {
				t.Fatalf("catalog requests=%d, want 1", catalogCalls)
			}
		})
	}
}

func TestHealthResourcesPastExtendedSupportRemainSeparateFromUnknown(t *testing.T) {
	now := time.Date(2029, 2, 1, 0, 0, 0, 0, time.UTC)
	catalog := NewHealthLifecycleCatalog()
	catalog.newClient = func(aws.Config) HealthAPI {
		return fakeHealthAPI(func(context.Context, *health.DescribeServiceLifecycleInput) (*health.DescribeServiceLifecycleOutput, error) {
			entries := testHealthEntries()
			for service, version := range map[string]string{"ELASTICACHE": "redis/3.2", "OPENSEARCH": "elasticsearch/5.1"} {
				entries = append(entries, healthtypes.ServiceLifecycle{
					Service: aws.String(service), Version: aws.String(version),
					LifecycleEvents: []healthtypes.LifecycleEvent{healthEvent("end_of_life", now, "all")},
				})
			}
			for service, version := range map[string]string{"ELASTICACHE": "redis/91.0", "OPENSEARCH": "opensearch/91.1"} {
				entries = append(entries, healthtypes.ServiceLifecycle{
					Service: aws.String(service), Version: aws.String(version),
					LifecycleEvents: []healthtypes.LifecycleEvent{{LifecycleEventType: aws.String("supported"), Regions: []string{"all"}}},
				})
			}
			return &health.DescribeServiceLifecycleOutput{ServiceLifecycles: entries}, nil
		})
	}
	cacheClient := fakeElastiCacheAPI{
		clusters: func(context.Context, *elasticache.DescribeCacheClustersInput) (*elasticache.DescribeCacheClustersOutput, error) {
			var caches []cachetypes.CacheCluster
			for name, version := range map[string]string{"supported-cache": "91.0.1", "eol-cache": "3.2.10", "past-cache": "5.0.6", "current-cache": "6.2.6", "standard-cache": "7.1", "unknown-cache": "90.1"} {
				cache := testCache(name)
				cache.EngineVersion = aws.String(version)
				caches = append(caches, cache)
			}
			return &elasticache.DescribeCacheClustersOutput{CacheClusters: caches}, nil
		},
		serverless: func(context.Context, *elasticache.DescribeServerlessCachesInput) (*elasticache.DescribeServerlessCachesOutput, error) {
			return &elasticache.DescribeServerlessCachesOutput{ServerlessCaches: []cachetypes.ServerlessCache{
				{ServerlessCacheName: aws.String("past-serverless"), Engine: aws.String("redis"), FullEngineVersion: aws.String("3.2.10"), MajorEngineVersion: aws.String("3")},
				{ServerlessCacheName: aws.String("supported-serverless"), Engine: aws.String("redis"), MajorEngineVersion: aws.String("91.0")},
			}}, nil
		},
	}
	domains := map[string]string{"supported-domain": "OpenSearch_91.1", "eol-domain": "Elasticsearch_5.1", "past-domain": "OpenSearch_1.2", "current-domain": "OpenSearch_1.3", "standard-domain": "OpenSearch_3.1", "unknown-domain": "OpenSearch_90.1"}
	searchClient := fakeOpenSearchAPI{
		list: func(context.Context, *opensearch.ListDomainNamesInput) (*opensearch.ListDomainNamesOutput, error) {
			var names []ostypes.DomainInfo
			for name := range domains {
				names = append(names, ostypes.DomainInfo{DomainName: aws.String(name)})
			}
			return &opensearch.ListDomainNamesOutput{DomainNames: names}, nil
		},
		describe: func(_ context.Context, in *opensearch.DescribeDomainInput) (*opensearch.DescribeDomainOutput, error) {
			name := aws.ToString(in.DomainName)
			return &opensearch.DescribeDomainOutput{DomainStatus: testOpenSearchDomain(name, domains[name])}, nil
		},
	}
	result, err := Fetch(context.Background(), Query{
		Targets:  []AccountTarget{{AccountID: "111111111111"}},
		Services: []Service{ServiceElastiCache, ServiceOpenSearch}, Now: now,
		HealthLifecycleCatalog: catalog, regionLister: fakeRegionLister{regions: []string{"us-east-1"}},
		elastiCacheLister: awsElastiCacheLister{catalog: catalog, newClient: func(aws.Config) ElastiCacheAPI { return cacheClient }},
		openSearchLister:  awsOpenSearchLister{catalog: catalog, newClient: func(aws.Config) OpenSearchAPI { return searchClient }},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Summary.TotalCount != 2 || len(result.Records) != 2 || result.Records[0].ResourceID != "current-cache" || result.Records[1].ResourceID != "current-domain" {
		t.Fatalf("current extended support = %+v", result)
	}
	if len(result.Summary.ByService) != 2 || result.Summary.ByService[0].Count != 1 || result.Summary.ByService[1].Count != 1 || len(result.Summary.ByAccount) != 1 || result.Summary.ByAccount[0].Count != 2 {
		t.Fatalf("totals include resources outside extended support: %+v", result.Summary)
	}
	past := result.Summary.PastExtendedSupportResources
	if len(past) != 5 || past[0].ResourceID != "eol-cache" || past[1].ResourceID != "past-cache" || past[2].ResourceID != "past-serverless" || past[3].ResourceID != "eol-domain" || past[4].ResourceID != "past-domain" {
		t.Fatalf("past extended support = %+v", past)
	}
	for _, record := range past {
		if record.EndOfLifeDate != nil {
			if !record.EndOfLifeDate.Equal(now) || !record.StandardSupportEndDate.IsZero() || record.ExtendedSupportEndDate != nil {
				t.Fatalf("end of life resource has fabricated support dates: %+v", record)
			}
			continue
		}
		if record.ExtendedSupportEndDate == nil || now.Before(*record.ExtendedSupportEndDate) || record.EngineVersion == "" || record.StandardSupportEndDate.IsZero() {
			t.Fatalf("expired resource lost lifecycle information: %+v", record)
		}
	}
	unknown := result.Summary.UnclassifiedResources
	if len(unknown) != 2 || unknown[0].ResourceID != "unknown-cache" || unknown[1].ResourceID != "unknown-domain" {
		t.Fatalf("unknown resources = %+v", unknown)
	}
	// A missing ARN prevents tags from loading but must retain the expired cache.
	if len(result.Summary.SkippedRegions) != 1 || result.Summary.SkippedRegions[0].Service != ServiceElastiCache {
		t.Fatalf("tag warning = %+v", result.Summary.SkippedRegions)
	}
}
