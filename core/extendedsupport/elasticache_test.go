package extendedsupport

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/eks"
	ekstypes "github.com/aws/aws-sdk-go-v2/service/eks/types"
	"github.com/aws/aws-sdk-go-v2/service/elasticache"
	cachetypes "github.com/aws/aws-sdk-go-v2/service/elasticache/types"
)

func TestElastiCacheLifecycleClassification(t *testing.T) {
	for _, tc := range []struct {
		name, engine, version, at string
		record                    bool
		warning                   string
	}{
		{"redis4", "redis", "4.0.10", "2026-10-19T00:00:00Z", true, ""},
		{"redis5", " REDIS ", " 5.0.6 ", "2026-10-19T00:00:00Z", true, ""},
		{"old patch still in expired major", "redis", "5.0.5", "2026-10-19T00:00:00Z", true, ""},
		{"standard final day", "redis", "4.0.10", "2026-01-31T23:59:59Z", false, ""},
		{"extended start", "redis", "4.0.10", "2026-02-01T00:00:00Z", true, ""},
		{"extended final day", "redis", "5.0.6", "2029-01-31T23:59:59Z", true, ""},
		{"extended ended", "redis", "5.0.6", "2029-02-01T00:00:00Z", false, "past extended support"},
		{"redis6 standard", "redis", "6.2.6", "2026-10-19T00:00:00Z", false, ""},
		{"redis6 last standard day", "redis", "6.2.6", "2027-01-31T23:59:59Z", false, ""},
		{"redis6 extended", "redis", "6.2.6", "2027-02-01T00:00:00Z", true, ""},
		{"redis6 final extended day", "redis", "6.2.6", "2030-01-31T23:59:59Z", true, ""},
		{"redis6 ended", "redis", "6.2.6", "2030-02-01T00:00:00Z", false, "past extended support"},
		{"utc boundary", "redis", "5.0.6", "2026-01-31T20:00:00-05:00", true, ""},
		{"redis7 standard", "redis", "7.1", "2026-10-19T00:00:00Z", false, ""},
		{"valkey", "valkey", "8.0", "2026-10-19T00:00:00Z", false, ""},
		{"memcached", "memcached", "1.6.22", "2026-10-19T00:00:00Z", false, ""},
		{"engine missing", "", "5.0.6", "2026-10-19T00:00:00Z", false, "engine is missing"},
		{"version missing", "redis", "", "2026-10-19T00:00:00Z", false, "version is missing"},
		{"version malformed", "redis", "5.bad", "2026-10-19T00:00:00Z", false, "version is invalid"},
		{"version incomplete", "redis", "5.", "2026-10-19T00:00:00Z", false, "version is invalid"},
		{"new version unknown", "redis", "8.0", "2026-10-19T00:00:00Z", false, "lifecycle information"},
		{"old version unknown", "redis", "3.2.10", "2026-10-19T00:00:00Z", false, "lifecycle information"},
		{"engine unknown", "new-engine", "5.0.6", "2026-10-19T00:00:00Z", false, "lifecycle information"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			at, err := time.Parse(time.RFC3339, tc.at)
			if err != nil {
				t.Fatal(err)
			}
			record, warning := classifyElastiCache(elastiCacheResource{id: "cache", engine: tc.engine, version: tc.version, resourceType: ResourceTypeElastiCacheCluster}, "111111111111", "us-east-1", at, testHealthCatalog(t))
			if (record != nil) != tc.record {
				t.Fatalf("record=%+v warning=%+v", record, warning)
			}
			if tc.warning == "" {
				if warning != nil {
					t.Fatal(warning)
				}
			} else if warning == nil || !strings.Contains(warning.Message, tc.warning) {
				t.Fatalf("warning=%+v, want %q", warning, tc.warning)
			}
			if record != nil && (record.Service != ServiceElastiCache || record.ResourceID != "cache" || record.Engine != "redis" || record.ExtendedSupportEndDate == nil || record.StandardSupportEndDate.Day() != 31 || record.MajorEngineVersion != strings.Split(strings.TrimSpace(tc.version), ".")[0]) {
				t.Fatal(record)
			}
		})
	}
	_, warning := classifyElastiCache(elastiCacheResource{engine: "redis", version: "5.0.6"}, "111111111111", "us-east-1", eksTestNow, testHealthCatalog(t))
	if warning == nil || !strings.Contains(warning.Message, "identifier is missing") {
		t.Fatal(warning)
	}
}

type fakeElastiCacheAPI struct {
	clusters   func(context.Context, *elasticache.DescribeCacheClustersInput) (*elasticache.DescribeCacheClustersOutput, error)
	serverless func(context.Context, *elasticache.DescribeServerlessCachesInput) (*elasticache.DescribeServerlessCachesOutput, error)
	tags       func(context.Context, *elasticache.ListTagsForResourceInput) (*elasticache.ListTagsForResourceOutput, error)
}

func (f fakeElastiCacheAPI) DescribeCacheClusters(ctx context.Context, in *elasticache.DescribeCacheClustersInput, _ ...func(*elasticache.Options)) (*elasticache.DescribeCacheClustersOutput, error) {
	if f.clusters != nil {
		return f.clusters(ctx, in)
	}
	return &elasticache.DescribeCacheClustersOutput{}, nil
}
func (f fakeElastiCacheAPI) DescribeServerlessCaches(ctx context.Context, in *elasticache.DescribeServerlessCachesInput, _ ...func(*elasticache.Options)) (*elasticache.DescribeServerlessCachesOutput, error) {
	if f.serverless != nil {
		return f.serverless(ctx, in)
	}
	return &elasticache.DescribeServerlessCachesOutput{}, nil
}
func (f fakeElastiCacheAPI) ListTagsForResource(ctx context.Context, in *elasticache.ListTagsForResourceInput, _ ...func(*elasticache.Options)) (*elasticache.ListTagsForResourceOutput, error) {
	if f.tags != nil {
		return f.tags(ctx, in)
	}
	return &elasticache.ListTagsForResourceOutput{}, nil
}

func testCache(id string) cachetypes.CacheCluster {
	return cachetypes.CacheCluster{CacheClusterId: aws.String(id), ARN: aws.String("arn:aws:elasticache:us-east-1:111111111111:cluster:" + id), Engine: aws.String("redis"), EngineVersion: aws.String("5.0.6"), ReplicationGroupId: aws.String("group")}
}

func TestElastiCachePaginationAndPartialDiscovery(t *testing.T) {
	clusterCalls, serverlessCalls, tagCalls := 0, 0, 0
	client := fakeElastiCacheAPI{
		clusters: func(_ context.Context, in *elasticache.DescribeCacheClustersInput) (*elasticache.DescribeCacheClustersOutput, error) {
			clusterCalls++
			if in.ShowCacheClustersNotInReplicationGroups != nil || in.CacheClusterId != nil {
				t.Fatal("replication group members excluded")
			}
			switch clusterCalls {
			case 1:
				return &elasticache.DescribeCacheClustersOutput{Marker: aws.String("empty")}, nil
			case 2:
				if aws.ToString(in.Marker) != "empty" {
					t.Fatal(in)
				}
				return &elasticache.DescribeCacheClustersOutput{CacheClusters: []cachetypes.CacheCluster{testCache("shard-001"), testCache("shard-001"), testCache("shard-002")}, Marker: aws.String("next")}, nil
			default:
				return nil, errors.New("AccessDenied on remaining caches")
			}
		},
		serverless: func(_ context.Context, in *elasticache.DescribeServerlessCachesInput) (*elasticache.DescribeServerlessCachesOutput, error) {
			serverlessCalls++
			if serverlessCalls == 1 {
				return &elasticache.DescribeServerlessCachesOutput{NextToken: aws.String("serverless-next")}, nil
			}
			if aws.ToString(in.NextToken) != "serverless-next" {
				t.Fatal(in)
			}
			return &elasticache.DescribeServerlessCachesOutput{ServerlessCaches: []cachetypes.ServerlessCache{
				{ServerlessCacheName: aws.String("current"), Engine: aws.String("redis"), MajorEngineVersion: aws.String("7")},
				{ServerlessCacheName: aws.String("unknown"), Engine: aws.String("redis")},
			}}, nil
		},
		tags: func(_ context.Context, in *elasticache.ListTagsForResourceInput) (*elasticache.ListTagsForResourceOutput, error) {
			tagCalls++
			if strings.HasSuffix(aws.ToString(in.ResourceName), "shard-002") {
				return nil, errors.New("tags denied")
			}
			return &elasticache.ListTagsForResourceOutput{TagList: []cachetypes.Tag{{Key: aws.String("team"), Value: aws.String("finops")}}}, nil
		},
	}
	lister := awsElastiCacheLister{catalog: testHealthCatalog(t), newClient: func(cfg aws.Config) ElastiCacheAPI {
		if cfg.Region != "us-east-1" {
			t.Fatal(cfg.Region)
		}
		return client
	}}
	result, err := lister.ListElastiCacheExtendedSupport(context.Background(), aws.Config{Region: "us-west-2"}, "us-east-1", "111111111111", eksTestNow)
	if err != nil || clusterCalls != 3 || serverlessCalls != 2 || tagCalls != 2 || len(result.Records) != 2 || len(result.Warnings) != 2 || len(result.UnclassifiedResources) != 1 {
		t.Fatalf("result=%+v err=%v calls=%d/%d/%d", result, err, clusterCalls, serverlessCalls, tagCalls)
	}
	if result.Records[0].Tags["team"] != "finops" || len(result.Records[1].Tags) != 0 || result.UnclassifiedResources[0].ResourceType != ResourceTypeElastiCacheServerless {
		t.Fatal(result)
	}
	for _, warning := range result.Warnings {
		if warning.Service != ServiceElastiCache || strings.TrimSpace(warning.Message) == "" {
			t.Fatal(warning)
		}
	}
}

func TestElastiCacheDiscoveryFailureDiagnostics(t *testing.T) {
	for _, operation := range []string{"clusters", "serverless"} {
		for _, failure := range []string{"nil", "blank error", "token cycle"} {
			t.Run(operation+"/"+failure, func(t *testing.T) {
				calls := 0
				next := func() (*string, error) {
					calls++
					if failure == "blank error" {
						return nil, errors.New(" ")
					}
					return aws.String("repeat"), nil
				}
				client := fakeElastiCacheAPI{}
				if operation == "clusters" {
					client.clusters = func(context.Context, *elasticache.DescribeCacheClustersInput) (*elasticache.DescribeCacheClustersOutput, error) {
						if failure == "nil" {
							calls++
							return nil, nil
						}
						token, err := next()
						return &elasticache.DescribeCacheClustersOutput{CacheClusters: []cachetypes.CacheCluster{testCache("cache")}, Marker: token}, err
					}
				} else {
					client.serverless = func(context.Context, *elasticache.DescribeServerlessCachesInput) (*elasticache.DescribeServerlessCachesOutput, error) {
						if failure == "nil" {
							calls++
							return nil, nil
						}
						token, err := next()
						return &elasticache.DescribeServerlessCachesOutput{NextToken: token}, err
					}
				}
				result, err := (awsElastiCacheLister{catalog: testHealthCatalog(t), newClient: func(aws.Config) ElastiCacheAPI { return client }}).ListElastiCacheExtendedSupport(context.Background(), aws.Config{}, "us-east-1", "111111111111", eksTestNow)
				if err != nil || len(result.Warnings) != 1 || calls > 2 || !strings.Contains(result.Warnings[0].Message, "ElastiCache.Describe") || !strings.Contains(result.Warnings[0].Message, "discovery incomplete") {
					t.Fatalf("calls=%d result=%+v err=%v", calls, result, err)
				}
				if failure == "token cycle" && !strings.Contains(result.Warnings[0].Message, "repeated pagination token") {
					t.Fatal(result.Warnings)
				}
			})
		}
	}
}

func TestElastiCacheTagFailuresRetainResources(t *testing.T) {
	for _, failure := range []string{"arn missing", "nil response", "error"} {
		t.Run(failure, func(t *testing.T) {
			cache := testCache("cache")
			if failure == "arn missing" {
				cache.ARN = nil
			}
			client := fakeElastiCacheAPI{
				clusters: func(context.Context, *elasticache.DescribeCacheClustersInput) (*elasticache.DescribeCacheClustersOutput, error) {
					return &elasticache.DescribeCacheClustersOutput{CacheClusters: []cachetypes.CacheCluster{cache}}, nil
				},
				tags: func(context.Context, *elasticache.ListTagsForResourceInput) (*elasticache.ListTagsForResourceOutput, error) {
					if failure == "arn missing" {
						t.Fatal("tag call without ARN")
					}
					if failure == "error" {
						return nil, errors.New(" ")
					}
					return nil, nil
				},
			}
			result, err := (awsElastiCacheLister{catalog: testHealthCatalog(t), newClient: func(aws.Config) ElastiCacheAPI { return client }}).ListElastiCacheExtendedSupport(context.Background(), aws.Config{}, "us-east-1", "111111111111", eksTestNow)
			if err != nil || len(result.Records) != 1 || len(result.Warnings) != 1 || len(result.UnclassifiedResources) != 0 || !strings.Contains(result.Warnings[0].Message, "cache") || !strings.Contains(result.Warnings[0].Message, "retained without tags") {
				t.Fatalf("result=%+v err=%v", result, err)
			}
		})
	}
}

func TestElastiCacheCancellationStopsScan(t *testing.T) {
	for _, operation := range []string{"clusters", "serverless", "tags"} {
		t.Run(operation, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			client := fakeElastiCacheAPI{
				clusters: func(context.Context, *elasticache.DescribeCacheClustersInput) (*elasticache.DescribeCacheClustersOutput, error) {
					if operation == "clusters" {
						cancel()
						return nil, ctx.Err()
					}
					return &elasticache.DescribeCacheClustersOutput{CacheClusters: []cachetypes.CacheCluster{testCache("cache")}}, nil
				},
				serverless: func(context.Context, *elasticache.DescribeServerlessCachesInput) (*elasticache.DescribeServerlessCachesOutput, error) {
					if operation == "serverless" {
						cancel()
						return nil, ctx.Err()
					}
					return &elasticache.DescribeServerlessCachesOutput{}, nil
				},
				tags: func(context.Context, *elasticache.ListTagsForResourceInput) (*elasticache.ListTagsForResourceOutput, error) {
					if operation == "tags" {
						cancel()
						return nil, ctx.Err()
					}
					return &elasticache.ListTagsForResourceOutput{}, nil
				},
			}
			_, err := (awsElastiCacheLister{catalog: testHealthCatalog(t), newClient: func(aws.Config) ElastiCacheAPI { return client }}).ListElastiCacheExtendedSupport(ctx, aws.Config{}, "us-east-1", "111111111111", eksTestNow)
			if !errors.Is(err, context.Canceled) {
				t.Fatal(err)
			}
		})
	}
}

func TestElastiCacheFetchAndRetryPreserveOtherServices(t *testing.T) {
	for _, denyRDS := range []bool{false, true} {
		t.Run(fmt.Sprint("rds-denied=", denyRDS), func(t *testing.T) {
			var rdsErr error
			if denyRDS {
				rdsErr = errors.New("RDS denied")
			}
			client := fakeElastiCacheAPI{
				clusters: func(context.Context, *elasticache.DescribeCacheClustersInput) (*elasticache.DescribeCacheClustersOutput, error) {
					return &elasticache.DescribeCacheClustersOutput{CacheClusters: []cachetypes.CacheCluster{testCache("cache")}}, nil
				},
				serverless: func(context.Context, *elasticache.DescribeServerlessCachesInput) (*elasticache.DescribeServerlessCachesOutput, error) {
					return nil, errors.New("serverless denied")
				},
			}
			catalog := NewEKSVersionCatalog()
			catalog.newClient = func(aws.Config) EKSAPI {
				return fakeEKSAPI{
					list: func(context.Context, *eks.ListClustersInput) (*eks.ListClustersOutput, error) {
						return &eks.ListClustersOutput{Clusters: []string{"kubernetes"}}, nil
					},
					describe: func(context.Context, *eks.DescribeClusterInput) (*eks.DescribeClusterOutput, error) {
						return &eks.DescribeClusterOutput{Cluster: &ekstypes.Cluster{Version: aws.String("1.31")}}, nil
					},
					versions: func(context.Context, *eks.DescribeClusterVersionsInput) (*eks.DescribeClusterVersionsOutput, error) {
						return &eks.DescribeClusterVersionsOutput{ClusterVersions: []ekstypes.ClusterVersionInformation{testEKSVersion("1.31")}}, nil
					},
				}
			}
			q := Query{Targets: []AccountTarget{{AccountID: "111111111111"}}, Services: []Service{ServiceRDS, ServiceEKS, ServiceElastiCache}, Now: eksTestNow, EKSVersionCatalog: catalog, HealthLifecycleCatalog: testHealthCatalog(t), regionLister: fakeRegionLister{regions: []string{"us-east-1"}}, rdsLister: fakeRDSLister{records: []Record{{AccountID: "111111111111", Region: "us-east-1", Service: ServiceRDS, ResourceID: "database"}}, err: rdsErr}, elastiCacheLister: awsElastiCacheLister{catalog: testHealthCatalog(t), newClient: func(aws.Config) ElastiCacheAPI { return client }}}
			result, err := Fetch(context.Background(), q)
			wantWarnings := 1
			if denyRDS {
				wantWarnings++
			}
			if err != nil || result.Summary.TotalCount != 3 || len(result.Summary.ByService) != 3 || len(result.Summary.SkippedRegions) != wantWarnings {
				t.Fatalf("result=%+v err=%v", result, err)
			}
			result.Summary.SkippedAccounts = []AccountWarning{{AccountID: "111111111111", Message: "initial credential failure"}}
			result, err = RetrySkippedAccounts(context.Background(), q, result)
			if err != nil || result.Summary.TotalCount != 3 || len(result.Summary.SkippedAccounts) != 0 {
				t.Fatalf("result=%+v err=%v", result, err)
			}
		})
	}
}
