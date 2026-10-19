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
	"github.com/aws/aws-sdk-go-v2/service/opensearch"
	ostypes "github.com/aws/aws-sdk-go-v2/service/opensearch/types"
)

func TestOpenSearchLifecycleClassification(t *testing.T) {
	for _, tc := range []struct {
		version, at  string
		record       bool
		warning      string
		extendedYear int
	}{
		{"Elasticsearch_1.5", "2026-10-19T00:00:00Z", true, "", 2027},
		{"Elasticsearch_5.6", "2026-10-19T00:00:00Z", true, "", 2028},
		{"Elasticsearch_7.8", "2026-10-19T00:00:00Z", true, "", 2027},
		{"OpenSearch_1.2", "2026-10-19T00:00:00Z", true, "", 2027},
		{" OpenSearch_2.9 ", "2026-10-19T00:00:00Z", true, "", 2027},
		{"OpenSearch_1.2", "2025-11-07T23:59:59Z", false, "", 0},
		{"OpenSearch_1.2", "2025-11-08T00:00:00Z", true, "", 2027},
		{"OpenSearch_1.2", "2026-11-08T00:00:00Z", true, "", 2027},
		{"OpenSearch_1.2", "2027-11-07T23:59:59Z", true, "", 2027},
		{"OpenSearch_1.2", "2027-11-08T00:00:00Z", false, "past extended support", 0},
		{"Elasticsearch_5.6", "2028-11-07T23:59:59Z", true, "", 2028},
		{"Elasticsearch_5.6", "2028-11-08T00:00:00Z", false, "past extended support", 0},
		{"OpenSearch_2.9", "2025-11-07T20:00:00-05:00", true, "", 2027},
		{"OpenSearch_2.11", "2026-10-19T00:00:00Z", false, "", 0},
		{"OpenSearch_2.11", "2027-11-08T00:00:00Z", true, "", 2028},
		{"OpenSearch_2.19", "2027-11-08T00:00:00Z", true, "", 2030},
		{"OpenSearch_1.3", "2027-11-08T00:00:00Z", true, "", 2030},
		{"Elasticsearch_6.8", "2027-11-08T00:00:00Z", true, "", 2030},
		{"Elasticsearch_7.9", "2027-11-08T00:00:00Z", true, "", 2028},
		{"Elasticsearch_7.10", "2027-11-08T00:00:00Z", true, "", 2030},
		{"OpenSearch_3.1", "2026-10-19T00:00:00Z", false, "", 0},
		{"OpenSearch_3.5", "2026-10-19T00:00:00Z", false, "", 0},
		{"", "2026-10-19T00:00:00Z", false, "version is missing", 0},
		{"1.2", "2026-10-19T00:00:00Z", false, "version is invalid", 0},
		{"Other_1.2", "2026-10-19T00:00:00Z", false, "version is invalid", 0},
		{"OpenSearch_1.bad", "2026-10-19T00:00:00Z", false, "version is invalid", 0},
		{"OpenSearch_1.2.3", "2026-10-19T00:00:00Z", false, "version is invalid", 0},
		{"OpenSearch_2.10", "2026-10-19T00:00:00Z", false, "lifecycle information", 0},
		{"OpenSearch_4.1", "2026-10-19T00:00:00Z", false, "lifecycle information", 0},
	} {
		t.Run(tc.version+"/"+tc.at, func(t *testing.T) {
			at, err := time.Parse(time.RFC3339, tc.at)
			if err != nil {
				t.Fatal(err)
			}
			record, warning := classifyOpenSearch("111111111111", "us-east-1", "domain", tc.version, at, testHealthCatalog(t))
			if (record != nil) != tc.record {
				t.Fatalf("record=%+v warning=%+v", record, warning)
			}
			if tc.warning == "" {
				if warning != nil {
					t.Fatal(warning)
				}
			} else if warning == nil || !strings.Contains(warning.Message, tc.warning) {
				t.Fatalf("warning=%+v want=%s", warning, tc.warning)
			}
			if record != nil {
				parts := strings.Split(strings.TrimSpace(tc.version), "_")
				if record.Service != ServiceOpenSearch || record.ResourceType != ResourceTypeOpenSearchDomain || record.ResourceID != "domain" || record.Engine != strings.ToLower(parts[0]) || record.EngineVersion != parts[1] || record.MajorEngineVersion != parts[1] || record.ExtendedSupportEndDate.Year() != tc.extendedYear {
					t.Fatal(record)
				}
			}
		})
	}
}

type fakeOpenSearchAPI struct {
	list     func(context.Context, *opensearch.ListDomainNamesInput) (*opensearch.ListDomainNamesOutput, error)
	describe func(context.Context, *opensearch.DescribeDomainInput) (*opensearch.DescribeDomainOutput, error)
	tags     func(context.Context, *opensearch.ListTagsInput) (*opensearch.ListTagsOutput, error)
}

func (f fakeOpenSearchAPI) ListDomainNames(ctx context.Context, in *opensearch.ListDomainNamesInput, _ ...func(*opensearch.Options)) (*opensearch.ListDomainNamesOutput, error) {
	if f.list != nil {
		return f.list(ctx, in)
	}
	return &opensearch.ListDomainNamesOutput{}, nil
}
func (f fakeOpenSearchAPI) DescribeDomain(ctx context.Context, in *opensearch.DescribeDomainInput, _ ...func(*opensearch.Options)) (*opensearch.DescribeDomainOutput, error) {
	if f.describe != nil {
		return f.describe(ctx, in)
	}
	return &opensearch.DescribeDomainOutput{DomainStatus: testOpenSearchDomain(aws.ToString(in.DomainName), "OpenSearch_1.2")}, nil
}
func (f fakeOpenSearchAPI) ListTags(ctx context.Context, in *opensearch.ListTagsInput, _ ...func(*opensearch.Options)) (*opensearch.ListTagsOutput, error) {
	if f.tags != nil {
		return f.tags(ctx, in)
	}
	return &opensearch.ListTagsOutput{}, nil
}

func testOpenSearchDomain(name, version string) *ostypes.DomainStatus {
	return &ostypes.DomainStatus{DomainName: aws.String(name), EngineVersion: aws.String(version), ARN: aws.String("arn:aws:es:us-east-1:111111111111:domain/" + name)}
}

func TestOpenSearchRetainsFindingsAcrossDomainAndTagFailures(t *testing.T) {
	descriptions, tagCalls := 0, 0
	client := fakeOpenSearchAPI{
		list: func(_ context.Context, in *opensearch.ListDomainNamesInput) (*opensearch.ListDomainNamesOutput, error) {
			if in.EngineType != "" {
				t.Fatal("engine filter excludes legacy domains")
			}
			var names []ostypes.DomainInfo
			for _, name := range []string{"old", "os-old", "standard", "unknown", "denied", "nil", "empty", "deleted", "missing", "wrong", "", "old"} {
				names = append(names, ostypes.DomainInfo{DomainName: aws.String(name)})
			}
			return &opensearch.ListDomainNamesOutput{DomainNames: names}, nil
		},
		describe: func(_ context.Context, in *opensearch.DescribeDomainInput) (*opensearch.DescribeDomainOutput, error) {
			descriptions++
			name := aws.ToString(in.DomainName)
			status := testOpenSearchDomain(name, "OpenSearch_1.2")
			switch name {
			case "old":
				status.EngineVersion = aws.String("Elasticsearch_7.8")
			case "standard":
				status.EngineVersion = aws.String("OpenSearch_2.19")
			case "unknown":
				status.EngineVersion = aws.String("OpenSearch_4.0")
			case "denied":
				return nil, errors.New(" ")
			case "nil":
				return nil, nil
			case "empty":
				return &opensearch.DescribeDomainOutput{}, nil
			case "deleted":
				status.Deleted = aws.Bool(true)
			case "missing":
				status.EngineVersion = nil
			case "wrong":
				status.DomainName = aws.String("different-domain")
			}
			return &opensearch.DescribeDomainOutput{DomainStatus: status}, nil
		},
		tags: func(_ context.Context, in *opensearch.ListTagsInput) (*opensearch.ListTagsOutput, error) {
			tagCalls++
			if strings.HasSuffix(aws.ToString(in.ARN), "/os-old") {
				return nil, errors.New("tags denied")
			}
			return &opensearch.ListTagsOutput{TagList: []ostypes.Tag{{Key: aws.String("team"), Value: aws.String("finops")}}}, nil
		},
	}
	cfg := aws.Config{Region: "us-west-2"}
	lister := awsOpenSearchLister{catalog: testHealthCatalog(t), newClient: func(regional aws.Config) OpenSearchAPI {
		if regional.Region != "us-east-1" {
			t.Fatal(regional.Region)
		}
		return client
	}}
	result, err := lister.ListOpenSearchExtendedSupport(context.Background(), cfg, "us-east-1", "111111111111", eksTestNow)
	if err != nil || cfg.Region != "us-west-2" || descriptions != 10 || tagCalls != 2 || len(result.Records) != 2 || len(result.Warnings) != 1 || len(result.UnclassifiedResources) != 7 {
		t.Fatalf("descriptions=%d tags=%d result=%+v err=%v", descriptions, tagCalls, result, err)
	}
	if result.Records[0].Tags["team"] != "finops" || result.Records[0].Engine != "elasticsearch" || len(result.Records[1].Tags) != 0 || !strings.Contains(result.Warnings[0].Message, "retained without tags") {
		t.Fatal(result)
	}
	for _, warning := range result.UnclassifiedResources {
		if warning.Service != ServiceOpenSearch || strings.TrimSpace(warning.Message) == "" {
			t.Fatal(warning)
		}
	}
}

func TestOpenSearchEmptyAndFailedDiscovery(t *testing.T) {
	for _, failure := range []string{"empty", "nil", "denied", "canceled"} {
		t.Run(failure, func(t *testing.T) {
			client := fakeOpenSearchAPI{list: func(context.Context, *opensearch.ListDomainNamesInput) (*opensearch.ListDomainNamesOutput, error) {
				switch failure {
				case "nil":
					return nil, nil
				case "denied":
					return nil, errors.New("AccessDenied")
				case "canceled":
					return nil, context.Canceled
				default:
					return &opensearch.ListDomainNamesOutput{}, nil
				}
			}}
			result, err := (awsOpenSearchLister{catalog: testHealthCatalog(t), newClient: func(aws.Config) OpenSearchAPI { return client }}).ListOpenSearchExtendedSupport(context.Background(), aws.Config{}, "us-east-1", "111111111111", eksTestNow)
			if len(result.Records) != 0 {
				t.Fatal(result)
			}
			if failure == "empty" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), "OpenSearch.ListDomainNames") {
				t.Fatal(err)
			}
			if failure == "canceled" && !errors.Is(err, context.Canceled) {
				t.Fatal(err)
			}
		})
	}
}

func TestOpenSearchMissingTagsDoNotDiscardDomains(t *testing.T) {
	for _, failure := range []string{"nil", "denied", "no ARN"} {
		t.Run(failure, func(t *testing.T) {
			client := fakeOpenSearchAPI{
				list: func(context.Context, *opensearch.ListDomainNamesInput) (*opensearch.ListDomainNamesOutput, error) {
					return &opensearch.ListDomainNamesOutput{DomainNames: []ostypes.DomainInfo{{DomainName: aws.String("old")}}}, nil
				},
				describe: func(context.Context, *opensearch.DescribeDomainInput) (*opensearch.DescribeDomainOutput, error) {
					domain := testOpenSearchDomain("old", "OpenSearch_1.2")
					if failure == "no ARN" {
						domain.ARN = nil
					}
					return &opensearch.DescribeDomainOutput{DomainStatus: domain}, nil
				},
				tags: func(context.Context, *opensearch.ListTagsInput) (*opensearch.ListTagsOutput, error) {
					if failure == "no ARN" {
						t.Fatal("tag request with no ARN")
					}
					if failure == "denied" {
						return nil, errors.New(" ")
					}
					return nil, nil
				},
			}
			result, err := (awsOpenSearchLister{catalog: testHealthCatalog(t), newClient: func(aws.Config) OpenSearchAPI { return client }}).ListOpenSearchExtendedSupport(context.Background(), aws.Config{}, "us-east-1", "111111111111", eksTestNow)
			if err != nil || len(result.Records) != 1 || len(result.Warnings) != 1 || len(result.UnclassifiedResources) != 0 || !strings.Contains(result.Warnings[0].Message, "OpenSearch.ListTags for old") {
				t.Fatalf("result=%+v err=%v", result, err)
			}
		})
	}
}

func TestOpenSearchCancellation(t *testing.T) {
	for _, operation := range []string{"list", "describe", "tags"} {
		t.Run(operation, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			client := fakeOpenSearchAPI{
				list: func(context.Context, *opensearch.ListDomainNamesInput) (*opensearch.ListDomainNamesOutput, error) {
					if operation == "list" {
						cancel()
					}
					return &opensearch.ListDomainNamesOutput{DomainNames: []ostypes.DomainInfo{{DomainName: aws.String("old")}}}, nil
				},
				describe: func(context.Context, *opensearch.DescribeDomainInput) (*opensearch.DescribeDomainOutput, error) {
					if operation == "describe" {
						cancel()
					}
					return &opensearch.DescribeDomainOutput{DomainStatus: testOpenSearchDomain("old", "OpenSearch_1.2")}, nil
				},
				tags: func(context.Context, *opensearch.ListTagsInput) (*opensearch.ListTagsOutput, error) {
					if operation == "tags" {
						cancel()
					}
					return &opensearch.ListTagsOutput{}, nil
				},
			}
			_, err := (awsOpenSearchLister{catalog: testHealthCatalog(t), newClient: func(aws.Config) OpenSearchAPI { return client }}).ListOpenSearchExtendedSupport(ctx, aws.Config{}, "us-east-1", "111111111111", eksTestNow)
			if !errors.Is(err, context.Canceled) {
				t.Fatal(err)
			}
		})
	}
}

func TestOpenSearchWorksAlongsideOtherServicesAndOnRetry(t *testing.T) {
	for _, failed := range []Service{"", ServiceRDS, ServiceOpenSearch} {
		t.Run(fmt.Sprint("failed=", failed), func(t *testing.T) {
			osClient := fakeOpenSearchAPI{list: func(context.Context, *opensearch.ListDomainNamesInput) (*opensearch.ListDomainNamesOutput, error) {
				if failed == ServiceOpenSearch {
					return nil, errors.New("OpenSearch denied")
				}
				return &opensearch.ListDomainNamesOutput{DomainNames: []ostypes.DomainInfo{{DomainName: aws.String("old")}}}, nil
			}}
			catalog := NewEKSVersionCatalog()
			catalog.newClient = func(aws.Config) EKSAPI {
				return fakeEKSAPI{
					list: func(context.Context, *eks.ListClustersInput) (*eks.ListClustersOutput, error) {
						return &eks.ListClustersOutput{Clusters: []string{"cluster"}}, nil
					},
					describe: func(context.Context, *eks.DescribeClusterInput) (*eks.DescribeClusterOutput, error) {
						return &eks.DescribeClusterOutput{Cluster: &ekstypes.Cluster{Version: aws.String("1.31")}}, nil
					},
					versions: func(context.Context, *eks.DescribeClusterVersionsInput) (*eks.DescribeClusterVersionsOutput, error) {
						return &eks.DescribeClusterVersionsOutput{ClusterVersions: []ekstypes.ClusterVersionInformation{testEKSVersion("1.31")}}, nil
					},
				}
			}
			rds := fakeRDSLister{records: []Record{{AccountID: "111111111111", Region: "us-east-1", Service: ServiceRDS, ResourceID: "database"}}}
			if failed == ServiceRDS {
				rds.records = nil
				rds.err = errors.New("RDS denied")
			}
			q := Query{Targets: []AccountTarget{{AccountID: "111111111111"}}, Services: []Service{ServiceRDS, ServiceEKS, ServiceElastiCache, ServiceOpenSearch}, Now: eksTestNow, Workers: 1, EKSVersionCatalog: catalog, HealthLifecycleCatalog: testHealthCatalog(t), regionLister: fakeRegionLister{regions: []string{"us-east-1"}}, rdsLister: rds, openSearchLister: awsOpenSearchLister{catalog: testHealthCatalog(t), newClient: func(aws.Config) OpenSearchAPI { return osClient }}, elastiCacheLister: awsElastiCacheLister{catalog: testHealthCatalog(t), newClient: func(aws.Config) ElastiCacheAPI {
				return fakeElastiCacheAPI{clusters: func(context.Context, *elasticache.DescribeCacheClustersInput) (*elasticache.DescribeCacheClustersOutput, error) {
					return &elasticache.DescribeCacheClustersOutput{CacheClusters: []cachetypes.CacheCluster{testCache("cache")}}, nil
				}}
			}}}
			result, err := Fetch(context.Background(), q)
			wantCount, wantWarnings := 4, 0
			if failed != "" {
				wantCount--
				wantWarnings++
			}
			if err != nil || result.Summary.TotalCount != wantCount || len(result.Summary.ByService) != wantCount || len(result.Summary.SkippedRegions) != wantWarnings {
				t.Fatalf("result=%+v err=%v", result, err)
			}
			if failed != "" && result.Summary.SkippedRegions[0].Service != failed {
				t.Fatal(result.Summary.SkippedRegions)
			}
			// An account skipped during credential loading is retried with the same
			// service set, timestamp, and catalog. Existing findings are deduplicated.
			result.Summary.SkippedAccounts = []AccountWarning{{AccountID: "111111111111", Message: "credential failure"}}
			result, err = RetrySkippedAccounts(context.Background(), q, result)
			if err != nil || result.Summary.TotalCount != wantCount || len(result.Summary.SkippedAccounts) != 0 {
				t.Fatalf("retry=%+v err=%v", result, err)
			}
		})
	}
}
