package extendedsupport

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/elasticache"
	cachetypes "github.com/aws/aws-sdk-go-v2/service/elasticache/types"
	"github.com/aws/aws-sdk-go-v2/service/health"
	healthtypes "github.com/aws/aws-sdk-go-v2/service/health/types"
)

type fakeHealthAPI func(context.Context, *health.DescribeServiceLifecycleInput) (*health.DescribeServiceLifecycleOutput, error)

func (f fakeHealthAPI) DescribeServiceLifecycle(ctx context.Context, in *health.DescribeServiceLifecycleInput, _ ...func(*health.Options)) (*health.DescribeServiceLifecycleOutput, error) {
	return f(ctx, in)
}

func healthEvent(kind string, date time.Time, regions ...string) healthtypes.LifecycleEvent {
	return healthtypes.LifecycleEvent{LifecycleEventType: aws.String(kind), Date: &date, Regions: regions}
}

func healthEntry(service, version string, standard, extended time.Time) healthtypes.ServiceLifecycle {
	return healthtypes.ServiceLifecycle{Service: aws.String(service), Version: aws.String(version), LifecycleEvents: []healthtypes.LifecycleEvent{
		healthEvent("END_OF_STANDARD_SUPPORT", standard, "all"), healthEvent("END_OF_EXTENDED_SUPPORT", extended, "all"),
	}}
}

// Synthetic API fixtures keep inventory tests independent of live AWS credentials.
// Production has no bundled dates or special cases for particular engine versions.
func testHealthEntries() []healthtypes.ServiceLifecycle {
	date := func(year int, month time.Month, day int) time.Time {
		return time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
	}
	var entries []healthtypes.ServiceLifecycle
	for _, version := range []string{"4", "5"} {
		entries = append(entries, healthEntry("ELASTICACHE", "redis/"+version, date(2026, 2, 1).Add(-time.Nanosecond), date(2029, 2, 1)))
	}
	entries = append(entries, healthEntry("ELASTICACHE", "redis/6", date(2027, 2, 1).Add(-time.Nanosecond), date(2030, 2, 1)))
	for _, version := range []string{"redis/7", "valkey/8.0", "memcached/1.6"} {
		entries = append(entries, healthEntry("ELASTICACHE", version, date(2031, 1, 1), date(2034, 1, 1)))
	}
	add := func(standardYear, extendedYear int, versions ...string) {
		for _, version := range versions {
			entries = append(entries, healthEntry("OPENSEARCH", version, date(standardYear, 11, 8).Add(-time.Nanosecond), date(extendedYear, 11, 8)))
		}
	}
	add(2025, 2027, "Elasticsearch_1.5", "Elasticsearch_7.8", "OpenSearch_1.2", "OpenSearch_2.9")
	add(2025, 2028, "Elasticsearch_5.6")
	add(2027, 2030, "Elasticsearch_6.8", "Elasticsearch_7.10", "OpenSearch_1.3", "OpenSearch_2.19")
	add(2027, 2028, "Elasticsearch_7.9", "OpenSearch_2.11")
	add(2031, 2034, "OpenSearch_3.1", "OpenSearch_3.5")
	return entries
}

func testHealthCatalog(t *testing.T) *HealthLifecycleCatalog {
	t.Helper()
	catalog := NewHealthLifecycleCatalog()
	catalog.newClient = func(aws.Config) HealthAPI {
		return fakeHealthAPI(func(context.Context, *health.DescribeServiceLifecycleInput) (*health.DescribeServiceLifecycleOutput, error) {
			return &health.DescribeServiceLifecycleOutput{ServiceLifecycles: testHealthEntries()}, nil
		})
	}
	if err := catalog.load(context.Background(), aws.Config{}, "123456789012"); err != nil {
		t.Fatal(err)
	}
	return catalog
}

func TestHealthCatalogPagesAndRejectsIncompleteResults(t *testing.T) {
	for _, failure := range []string{"", "error", "nil", "cycle"} {
		t.Run(failure, func(t *testing.T) {
			calls := 0
			client := fakeHealthAPI(func(_ context.Context, in *health.DescribeServiceLifecycleInput) (*health.DescribeServiceLifecycleOutput, error) {
				calls++
				if in.Filter != nil || aws.ToInt32(in.MaxResults) != 20 {
					t.Fatal(in)
				}
				if calls == 1 {
					return &health.DescribeServiceLifecycleOutput{NextToken: aws.String("empty")}, nil
				}
				if calls == 2 {
					return &health.DescribeServiceLifecycleOutput{ServiceLifecycles: testHealthEntries(), NextToken: aws.String("next")}, nil
				}
				switch failure {
				case "error":
					return nil, errors.New("denied")
				case "nil":
					return nil, nil
				case "cycle":
					return &health.DescribeServiceLifecycleOutput{NextToken: aws.String("empty")}, nil
				}
				return &health.DescribeServiceLifecycleOutput{}, nil
			})
			entries, err := loadHealthLifecycles(context.Background(), client)
			if calls != 3 {
				t.Fatal(calls)
			}
			if failure == "" {
				if err != nil || len(entries) == 0 {
					t.Fatal(entries, err)
				}
			} else if err == nil || entries != nil || !strings.Contains(err.Error(), "Health.DescribeServiceLifecycle") {
				t.Fatal(entries, err)
			}
		})
	}
}

func TestHealthCatalogSharesConcurrentLoadsAndRetriesFailures(t *testing.T) {
	catalog := NewHealthLifecycleCatalog()
	var calls atomic.Int32
	started, release := make(chan struct{}), make(chan struct{})
	catalog.newClient = func(cfg aws.Config) HealthAPI {
		if cfg.Region != "us-east-1" {
			t.Error(cfg.Region)
		}
		return fakeHealthAPI(func(context.Context, *health.DescribeServiceLifecycleInput) (*health.DescribeServiceLifecycleOutput, error) {
			if calls.Add(1) == 1 {
				close(started)
				<-release
			}
			return &health.DescribeServiceLifecycleOutput{ServiceLifecycles: testHealthEntries()}, nil
		})
	}
	var wg sync.WaitGroup
	for range 12 {
		wg.Go(func() {
			if err := catalog.load(context.Background(), aws.Config{Region: "eu-west-1"}, "123456789012"); err != nil {
				t.Error(err)
			}
		})
	}
	<-started
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := catalog.load(ctx, aws.Config{}, "123456789012"); !errors.Is(err, context.Canceled) {
		t.Error(err)
	}
	close(release)
	wg.Wait()
	if calls.Load() != 1 {
		t.Fatal(calls.Load())
	}
	catalog.retryFailures()
	if err := catalog.load(context.Background(), aws.Config{}, "123456789012"); err != nil || calls.Load() != 1 {
		t.Fatal(err, calls.Load())
	}

	catalog = NewHealthLifecycleCatalog()
	count := 0
	catalog.newClient = func(aws.Config) HealthAPI {
		return fakeHealthAPI(func(context.Context, *health.DescribeServiceLifecycleInput) (*health.DescribeServiceLifecycleOutput, error) {
			count++
			if count == 1 {
				return nil, errors.New("SubscriptionRequiredException")
			}
			return &health.DescribeServiceLifecycleOutput{ServiceLifecycles: testHealthEntries()}, nil
		})
	}
	for range 2 {
		if err := catalog.load(context.Background(), aws.Config{}, "123456789012"); err == nil {
			t.Fatal("denied catalog succeeded")
		}
	}
	if count != 1 || len(catalog.warnings()) != 1 {
		t.Fatal(count, catalog.warnings())
	}
	if err := catalog.load(context.Background(), aws.Config{}, "987654321098"); err != nil || count != 2 || len(catalog.warnings()) != 0 {
		t.Fatal(err, count, catalog.warnings())
	}
}

func TestHealthLookupRecoversAfterCatalogLoad(t *testing.T) {
	catalog := NewHealthLifecycleCatalog()
	if _, err := catalog.lookup(ServiceElastiCache, "redis", "5.0.6", "us-east-1", eksTestNow); err == nil {
		t.Fatal("lookup succeeded without a catalog")
	}
	catalog.newClient = func(aws.Config) HealthAPI {
		return fakeHealthAPI(func(context.Context, *health.DescribeServiceLifecycleInput) (*health.DescribeServiceLifecycleOutput, error) {
			return &health.DescribeServiceLifecycleOutput{ServiceLifecycles: testHealthEntries()}, nil
		})
	}
	if err := catalog.load(context.Background(), aws.Config{}, "111111111111"); err != nil {
		t.Fatal(err)
	}
	lifecycle, err := catalog.lookup(ServiceElastiCache, "redis", "5.0.6", "us-east-1", eksTestNow)
	if err != nil || lifecycle.standardEnd == nil || lifecycle.extendedEnd == nil {
		t.Fatalf("lookup retained the pre-load failure: lifecycle=%+v err=%v", lifecycle, err)
	}
}

func TestHealthPreloadUsesCatalogCredentialsOnceWithoutAssumingMembers(t *testing.T) {
	catalog := NewHealthLifecycleCatalog()
	apiCalls, payerLoads, memberLoads := 0, 0, 0
	catalog.newClient = func(aws.Config) HealthAPI {
		return fakeHealthAPI(func(context.Context, *health.DescribeServiceLifecycleInput) (*health.DescribeServiceLifecycleOutput, error) {
			apiCalls++
			return &health.DescribeServiceLifecycleOutput{ServiceLifecycles: testHealthEntries()}, nil
		})
	}
	q := Query{}
	for _, account := range []string{"111111111111", "222222222222"} {
		q.Targets = append(q.Targets, AccountTarget{AccountID: account, CatalogAccountID: "123456789012", CatalogConfigLoader: func(context.Context) (aws.Config, error) { payerLoads++; return aws.Config{}, nil }, ConfigLoader: func(context.Context) (aws.Config, error) { memberLoads++; return aws.Config{}, nil }})
	}
	if err := catalog.preload(context.Background(), q); err != nil {
		t.Fatal(err)
	}
	if apiCalls != 1 || payerLoads != 1 || memberLoads != 0 {
		t.Fatal(apiCalls, payerLoads, memberLoads)
	}
}

func TestHealthFailureKeepsOtherFindingsAndExplainsUnclassifiedResources(t *testing.T) {
	catalog := NewHealthLifecycleCatalog()
	apiCalls := 0
	catalog.newClient = func(aws.Config) HealthAPI {
		return fakeHealthAPI(func(context.Context, *health.DescribeServiceLifecycleInput) (*health.DescribeServiceLifecycleOutput, error) {
			apiCalls++
			return nil, errors.New("AccessDenied")
		})
	}
	cacheClient := fakeElastiCacheAPI{clusters: func(context.Context, *elasticache.DescribeCacheClustersInput) (*elasticache.DescribeCacheClustersOutput, error) {
		return &elasticache.DescribeCacheClustersOutput{CacheClusters: []cachetypes.CacheCluster{testCache("cache")}}, nil
	}}
	q := Query{Targets: []AccountTarget{{AccountID: "111111111111", CatalogAccountID: "123456789012"}, {AccountID: "222222222222", CatalogAccountID: "123456789012"}}, Services: []Service{ServiceRDS, ServiceElastiCache}, Now: eksTestNow, HealthLifecycleCatalog: catalog, regionLister: fakeRegionLister{regions: []string{"us-east-1", "us-west-2"}}, rdsLister: fakeRDSLister{records: []Record{{AccountID: "111111111111", Region: "us-east-1", Service: ServiceRDS, ResourceID: "database"}}}, elastiCacheLister: awsElastiCacheLister{catalog: catalog, newClient: func(aws.Config) ElastiCacheAPI { return cacheClient }}}
	result, err := Fetch(context.Background(), q)
	if err != nil || apiCalls != 1 || len(result.Records) != 1 || len(result.Summary.LifecycleWarnings) != 1 || len(result.Summary.UnclassifiedResources) != 4 {
		t.Fatalf("calls=%d result=%+v err=%v", apiCalls, result, err)
	}
	for _, warning := range result.Summary.UnclassifiedResources {
		if !strings.Contains(warning.Message, "AccessDenied") {
			t.Fatal(warning)
		}
	}
	if !strings.Contains(result.Summary.LifecycleWarnings[0], "Health") {
		t.Fatal(result.Summary)
	}
}

func TestHealthRetryRefreshesFailedCatalogWithoutStaticFallback(t *testing.T) {
	catalog := NewHealthLifecycleCatalog()
	calls := 0
	catalog.newClient = func(aws.Config) HealthAPI {
		return fakeHealthAPI(func(context.Context, *health.DescribeServiceLifecycleInput) (*health.DescribeServiceLifecycleOutput, error) {
			calls++
			if calls == 1 {
				return nil, errors.New("expired")
			}
			return &health.DescribeServiceLifecycleOutput{ServiceLifecycles: testHealthEntries()}, nil
		})
	}
	q := Query{Targets: []AccountTarget{{AccountID: "111111111111"}}, Services: []Service{ServiceElastiCache}, Now: eksTestNow, HealthLifecycleCatalog: catalog, regionLister: fakeRegionLister{regions: []string{"us-east-1"}}, elastiCacheLister: awsElastiCacheLister{catalog: catalog, newClient: func(aws.Config) ElastiCacheAPI {
		return fakeElastiCacheAPI{clusters: func(context.Context, *elasticache.DescribeCacheClustersInput) (*elasticache.DescribeCacheClustersOutput, error) {
			return &elasticache.DescribeCacheClustersOutput{CacheClusters: []cachetypes.CacheCluster{testCache("cache")}}, nil
		}}
	}}}
	result, err := Fetch(context.Background(), q)
	if err != nil || len(result.Records) != 0 || len(result.Summary.LifecycleWarnings) != 1 {
		t.Fatal(result, err)
	}
	result.Summary.SkippedAccounts = []AccountWarning{{AccountID: "111111111111", Message: "credentials unavailable"}}
	result, err = RetrySkippedAccounts(context.Background(), q, result)
	if err != nil || calls != 2 || len(result.Records) != 1 || len(result.Summary.LifecycleWarnings) != 0 {
		t.Fatal(result, err, calls)
	}
}

func TestHealthMilestonesUseAPIInstantsAndRegionalOverrides(t *testing.T) {
	start := time.Date(2032, 3, 14, 9, 26, 0, 0, time.UTC)
	end := start.AddDate(2, 0, 0)
	for _, region := range []string{"us-east-1", "us-west-2"} {
		events := []healthtypes.LifecycleEvent{healthEvent("STANDARD_SUPPORT_END", start, "all"), healthEvent("EXTENDED_SUPPORT_END", end, "all"), healthEvent("STANDARD_SUPPORT_END", start.AddDate(1, 0, 0), "us-west-2")}
		lifecycle, err := parseHealthLifecycle(events, region, start)
		if err != nil {
			t.Fatal(err)
		}
		want := start
		if region == "us-west-2" {
			want = start.AddDate(1, 0, 0)
		}
		if !lifecycle.standardEnd.Equal(want) {
			t.Fatal(lifecycle)
		}
		catalog := &HealthLifecycleCatalog{entries: map[healthVersionKey][]healthtypes.LifecycleEvent{{service: ServiceElastiCache, engine: "redis", version: "99"}: events}}
		for _, offset := range []time.Duration{-time.Nanosecond, 0} {
			record, warning := classifyElastiCache(elastiCacheResource{id: "new", engine: "redis", version: "99.1", resourceType: ResourceTypeElastiCacheCluster}, "111111111111", region, want.Add(offset), catalog)
			if warning != nil || (record != nil) != (offset == 0) {
				t.Fatal(record, warning, offset)
			}
		}
	}
}

func TestHealthRejectsMissingAmbiguousAndConflictingMilestones(t *testing.T) {
	start := time.Date(2032, 1, 1, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(2, 0, 0)
	for _, tc := range []struct {
		name   string
		events []healthtypes.LifecycleEvent
		want   string
	}{
		{"empty", nil, "milestone is missing"},
		{"unknown type", []healthtypes.LifecycleEvent{healthEvent("NEW_UNDOCUMENTED_EVENT", start, "all")}, "milestone is missing"},
		{"generic alone", []healthtypes.LifecycleEvent{healthEvent("END_OF_SUPPORT", start, "all")}, "ambiguous"},
		{"billing alone", []healthtypes.LifecycleEvent{{LifecycleEventType: aws.String("PRICE_INCREASE"), Date: &start, ImpactRisks: []string{"BILLING"}, Regions: []string{"all"}}}, "milestone is missing"},
		{"missing date", []healthtypes.LifecycleEvent{{LifecycleEventType: aws.String("END_OF_STANDARD_SUPPORT"), Regions: []string{"all"}}}, "date is missing"},
		{"missing regions", []healthtypes.LifecycleEvent{healthEvent("END_OF_STANDARD_SUPPORT", start)}, "no region information"},
		{"other region", []healthtypes.LifecycleEvent{healthEvent("END_OF_STANDARD_SUPPORT", start, "eu-west-1")}, "milestone is missing"},
		{"conflict", []healthtypes.LifecycleEvent{healthEvent("END_OF_STANDARD_SUPPORT", start, "all"), healthEvent("END_OF_STANDARD_SUPPORT", end, "all")}, "conflicting"},
		{"reversed", []healthtypes.LifecycleEvent{healthEvent("END_OF_STANDARD_SUPPORT", end, "all"), healthEvent("END_OF_EXTENDED_SUPPORT", start, "all")}, "inconsistent"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseHealthLifecycle(tc.events, "us-east-1", start)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error=%v want=%s", err, tc.want)
			}
		})
	}
}

func TestHealthGenericMilestonesRequireContext(t *testing.T) {
	start := time.Date(2032, 1, 1, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(2, 0, 0)
	for _, events := range [][]healthtypes.LifecycleEvent{
		{healthEvent("END_OF_SUPPORT", start, "all"), healthEvent("END_OF_LIFE", end, "all")},
		{healthEvent("END_OF_STANDARD_SUPPORT", start, "all"), healthEvent("END_OF_SUPPORT", end, "all")},
		{{LifecycleEventType: aws.String("END_OF_SUPPORT"), Date: &start, ImpactRisks: []string{"BILLING"}, Regions: []string{"all"}}, healthEvent("END_OF_LIFE", end, "all")},
	} {
		lifecycle, err := parseHealthLifecycle(events, "us-east-1", start)
		terminal := lifecycle.extendedEnd
		if terminal == nil {
			terminal = lifecycle.endOfLife
		}
		if err != nil || lifecycle.standardEnd == nil || !lifecycle.standardEnd.Equal(start) || terminal == nil || !terminal.Equal(end) {
			t.Fatal(lifecycle, err)
		}
	}
}

func TestHealthEntryIdentifiers(t *testing.T) {
	for _, tc := range []struct{ service, version, title, engine, want string }{
		{"ELASTICACHE", "redis/6", "", "redis", "6"},
		{"ELASTICACHE", "6", "ElastiCache Redis OSS 6", "redis", "6"},
		{"ELASTICACHE", "valkey/8.2", "", "valkey", "8.2"},
		{"OPENSEARCH", "OpenSearch_2.19", "", "opensearch", "2.19"},
		{"ES", "elasticsearch/7.10", "", "elasticsearch", "7.10"},
		{"ES", "es/7.10", "Amazon OpenSearch Elasticsearch 7.10", "elasticsearch", "7.10"},
		{"ES", "es/7.10", "", "elasticsearch", "7.10"},
		{"ES", " ES/7.10 ", "", "elasticsearch", "7.10"},
		{"OPENSEARCH", "es_7.10", "", "elasticsearch", "7.10"},
		{"OPENSEARCH_SERVICE", "es:7.10", "", "elasticsearch", "7.10"},
		{"OPENSEARCH", "7.10", "Amazon Elasticsearch 7.10", "elasticsearch", "7.10"},
		{"ES", "7.10", "Amazon OpenSearch Elasticsearch 7.10", "elasticsearch", "7.10"},
		{"ES", "os/2.11", "Amazon OpenSearch 2.11", "opensearch", "2.11"},
		{"ES", "os/2.11", "", "opensearch", "2.11"},
		{"ES", " OS/2.11 ", "", "opensearch", "2.11"},
		{"OPENSEARCH", "os_2.11", "", "opensearch", "2.11"},
		{"OPENSEARCH_SERVICE", "os:2.11", "", "opensearch", "2.11"},
	} {
		t.Run(fmt.Sprint(tc), func(t *testing.T) {
			key, ok := healthEntryKey(healthtypes.ServiceLifecycle{Service: aws.String(tc.service), Version: aws.String(tc.version), Title: aws.String(tc.title)})
			if !ok || key.engine != tc.engine || key.version != tc.want {
				t.Fatal(key, ok)
			}
		})
	}
}

func TestHealthNewVersionsAndEnginesNeedNoBundledSchedule(t *testing.T) {
	start := time.Date(2032, 1, 1, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(2, 0, 0)
	catalog := NewHealthLifecycleCatalog()
	catalog.newClient = func(aws.Config) HealthAPI {
		return fakeHealthAPI(func(context.Context, *health.DescribeServiceLifecycleInput) (*health.DescribeServiceLifecycleOutput, error) {
			return &health.DescribeServiceLifecycleOutput{ServiceLifecycles: []healthtypes.ServiceLifecycle{healthEntry("ELASTICACHE", "valkey/90.2", start, end), healthEntry("OPENSEARCH", "opensearch/42.7", start, end)}}, nil
		})
	}
	if err := catalog.load(context.Background(), aws.Config{}, "123456789012"); err != nil {
		t.Fatal(err)
	}
	cache, warning := classifyElastiCache(elastiCacheResource{id: "future-cache", engine: "valkey", version: "90.2.1", resourceType: ResourceTypeElastiCacheCluster}, "111111111111", "us-east-1", start.Add(time.Hour), catalog)
	if warning != nil || cache == nil || cache.Engine != "valkey" {
		t.Fatal(cache, warning)
	}
	domain, warning := classifyOpenSearch("111111111111", "us-east-1", "future-domain", "OpenSearch_42.7", start.Add(time.Hour), catalog)
	if warning != nil || domain == nil || domain.EngineVersion != "42.7" {
		t.Fatal(domain, warning)
	}
}
