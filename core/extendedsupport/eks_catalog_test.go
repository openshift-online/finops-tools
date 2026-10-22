package extendedsupport

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/eks"
	ekstypes "github.com/aws/aws-sdk-go-v2/service/eks/types"
)

func testEKSCredentials(id string) aws.Config {
	return aws.Config{Credentials: credentials.NewStaticCredentialsProvider(id, "secret", "token")}
}

func TestEKSCatalogSharesConcurrentLoads(t *testing.T) {
	catalog := NewEKSVersionCatalog()
	entered, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	catalog.newClient = func(aws.Config) EKSAPI {
		return fakeEKSAPI{versions: func(context.Context, *eks.DescribeClusterVersionsInput) (*eks.DescribeClusterVersionsOutput, error) {
			if calls.Add(1) == 1 {
				close(entered)
			}
			<-release
			return &eks.DescribeClusterVersionsOutput{ClusterVersions: []ekstypes.ClusterVersionInformation{testEKSVersion("1.31")}}, nil
		}}
	}
	var wg sync.WaitGroup
	for i := 0; i < 40; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			result := catalog.load(context.Background(), aws.Config{}, "us-east-1", fmt.Sprint(i))
			if result.err != nil || len(result.versions) != 1 {
				t.Errorf("catalog = %+v", result)
			}
		}(i)
	}
	<-entered
	close(release)
	wg.Wait()
	if calls.Load() != 1 {
		t.Fatalf("catalog calls = %d", calls.Load())
	}
}

func TestEKSCatalogPreloadsBeforeMembersAndSurvivesAccountRetry(t *testing.T) {
	catalog := NewEKSVersionCatalog()
	var catalogCalls, described atomic.Int32
	catalog.newClient = func(cfg aws.Config) EKSAPI {
		return fakeEKSAPI{
			versions: func(context.Context, *eks.DescribeClusterVersionsInput) (*eks.DescribeClusterVersionsOutput, error) {
				creds, err := cfg.Credentials.Retrieve(context.Background())
				if err != nil || creds.AccessKeyID != "payer" {
					t.Errorf("catalog not preloaded through payer: %v %v", creds.AccessKeyID, err)
				}
				catalogCalls.Add(1)
				return &eks.DescribeClusterVersionsOutput{ClusterVersions: []ekstypes.ClusterVersionInformation{testEKSVersion("1.31")}}, nil
			},
			list: func(context.Context, *eks.ListClustersInput) (*eks.ListClustersOutput, error) {
				return &eks.ListClustersOutput{Clusters: []string{"cluster"}}, nil
			},
			describe: func(context.Context, *eks.DescribeClusterInput) (*eks.DescribeClusterOutput, error) {
				described.Add(1)
				return &eks.DescribeClusterOutput{Cluster: &ekstypes.Cluster{Version: aws.String("1.31")}}, nil
			},
		}
	}
	payerLoads := 0
	memberLoads := make(map[string]int)
	q := Query{Services: []Service{ServiceEKS}, Workers: 1, Now: eksTestNow, EKSVersionCatalog: catalog, regionLister: fakeRegionLister{regions: []string{"us-east-1", "us-west-2"}}}
	for _, id := range []string{"111111111111", "222222222222"} {
		q.Targets = append(q.Targets, AccountTarget{
			AccountID: id, CatalogAccountID: "123456789012",
			CatalogConfigLoader: func(context.Context) (aws.Config, error) { payerLoads++; return testEKSCredentials("payer"), nil },
			ConfigLoader: func(context.Context) (aws.Config, error) {
				if catalogCalls.Load() != 2 {
					t.Error("member role loaded before catalogs completed")
				}
				memberLoads[id]++
				if id == "222222222222" && memberLoads[id] == 1 {
					return aws.Config{}, errors.New("temporary authentication failure")
				}
				return testEKSCredentials(id), nil
			},
		})
	}
	initial, err := Fetch(context.Background(), q)
	if err != nil {
		t.Fatal(err)
	}
	if len(initial.Records) != 2 || len(initial.Summary.SkippedAccounts) != 1 {
		t.Fatalf("initial = %+v", initial)
	}
	result, err := RetrySkippedAccounts(context.Background(), q, initial)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Records) != 4 || result.Summary.TotalCount != 4 || len(result.Summary.SkippedAccounts) != 0 || catalogCalls.Load() != 2 || described.Load() != 4 || payerLoads != 2 || memberLoads["111111111111"] != 1 || memberLoads["222222222222"] != 2 {
		t.Fatalf("result=%+v catalogs=%d described=%d payers=%d members=%v", result, catalogCalls.Load(), described.Load(), payerLoads, memberLoads)
	}
}

func TestEKSCatalogPermissionFailuresDoNotPoisonOtherAccounts(t *testing.T) {
	catalog := NewEKSVersionCatalog()
	calls := make(map[string]int)
	catalog.newClient = func(cfg aws.Config) EKSAPI {
		creds, err := cfg.Credentials.Retrieve(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		id := creds.AccessKeyID
		return fakeEKSAPI{
			versions: func(context.Context, *eks.DescribeClusterVersionsInput) (*eks.DescribeClusterVersionsOutput, error) {
				calls[id]++
				if id == "payer" || id == "111111111111" {
					return nil, errors.New("AccessDenied for " + id)
				}
				return &eks.DescribeClusterVersionsOutput{ClusterVersions: []ekstypes.ClusterVersionInformation{testEKSVersion("1.31")}}, nil
			},
			list: func(context.Context, *eks.ListClustersInput) (*eks.ListClustersOutput, error) {
				return &eks.ListClustersOutput{Clusters: []string{"one", "two"}}, nil
			},
			describe: func(context.Context, *eks.DescribeClusterInput) (*eks.DescribeClusterOutput, error) {
				return &eks.DescribeClusterOutput{Cluster: &ekstypes.Cluster{Version: aws.String("1.31")}}, nil
			},
		}
	}
	q := Query{Services: []Service{ServiceEKS}, Workers: 1, Now: eksTestNow, EKSVersionCatalog: catalog, regionLister: fakeRegionLister{regions: []string{"us-east-1"}}}
	for _, id := range []string{"111111111111", "222222222222", "333333333333"} {
		q.Targets = append(q.Targets, AccountTarget{AccountID: id, AWSConfig: testEKSCredentials(id), CatalogAccountID: "123456789012", CatalogConfigLoader: func(context.Context) (aws.Config, error) { return testEKSCredentials("payer"), nil }})
	}
	result, err := Fetch(context.Background(), q)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Records) != 4 || len(result.Summary.UnclassifiedResources) != 2 || calls["payer"] != 1 || calls["111111111111"] != 1 || calls["222222222222"] != 1 || calls["333333333333"] != 0 {
		t.Fatalf("result=%+v calls=%v", result, calls)
	}
}

func TestEKSCatalogLoadsAdditionalMemberRegionsOnDemand(t *testing.T) {
	catalog := NewEKSVersionCatalog()
	var mu sync.Mutex
	calls := make(map[string]int)
	catalog.newClient = func(cfg aws.Config) EKSAPI {
		return fakeEKSAPI{
			versions: func(context.Context, *eks.DescribeClusterVersionsInput) (*eks.DescribeClusterVersionsOutput, error) {
				mu.Lock()
				calls[cfg.Region]++
				mu.Unlock()
				return &eks.DescribeClusterVersionsOutput{ClusterVersions: []ekstypes.ClusterVersionInformation{testEKSVersion("1.31")}}, nil
			},
			list: func(context.Context, *eks.ListClustersInput) (*eks.ListClustersOutput, error) {
				return &eks.ListClustersOutput{Clusters: []string{"cluster"}}, nil
			},
			describe: func(context.Context, *eks.DescribeClusterInput) (*eks.DescribeClusterOutput, error) {
				return &eks.DescribeClusterOutput{Cluster: &ekstypes.Cluster{Version: aws.String("1.31")}}, nil
			},
		}
	}
	result, err := Fetch(context.Background(), Query{
		Targets: []AccountTarget{{
			AccountID: "111111111111", CatalogAccountID: "123456789012",
			CatalogConfigLoader: func(context.Context) (aws.Config, error) { return testEKSCredentials("payer"), nil },
			ConfigLoader: func(context.Context) (aws.Config, error) {
				if calls["us-east-1"] != 1 || calls["us-west-2"] != 0 {
					t.Error("known regions were not preloaded before member authentication")
				}
				return testEKSCredentials("member"), nil
			},
		}},
		Services: []Service{ServiceEKS}, Now: eksTestNow, EKSVersionCatalog: catalog,
		regionLister: retryRegionLister{list: func(ctx context.Context, cfg aws.Config) ([]string, error) {
			creds, err := cfg.Credentials.Retrieve(ctx)
			if err != nil {
				return nil, err
			}
			if creds.AccessKeyID == "payer" {
				return []string{"us-east-1"}, nil
			}
			return []string{"us-east-1", "us-west-2"}, nil
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Records) != 2 || calls["us-east-1"] != 1 || calls["us-west-2"] != 1 {
		t.Fatalf("result=%+v catalog calls=%v", result, calls)
	}
}

func TestEKSCatalogPaginationAndFailureCaching(t *testing.T) {
	catalog := NewEKSVersionCatalog()
	pages := 0
	catalog.newClient = func(aws.Config) EKSAPI {
		return fakeEKSAPI{versions: func(_ context.Context, input *eks.DescribeClusterVersionsInput) (*eks.DescribeClusterVersionsOutput, error) {
			pages++
			if input.NextToken == nil {
				return &eks.DescribeClusterVersionsOutput{NextToken: aws.String("next")}, nil
			}
			return &eks.DescribeClusterVersionsOutput{ClusterVersions: []ekstypes.ClusterVersionInformation{testEKSVersion("1.31")}}, nil
		}}
	}
	for i := 0; i < 2; i++ {
		result := catalog.load(context.Background(), aws.Config{}, "us-east-1", "111111111111")
		if result.err != nil || len(result.versions) != 1 {
			t.Fatal(result)
		}
	}
	if pages != 2 {
		t.Fatalf("pages = %d", pages)
	}
	failed := NewEKSVersionCatalog()
	failures := 0
	failed.newClient = func(aws.Config) EKSAPI {
		return fakeEKSAPI{versions: func(context.Context, *eks.DescribeClusterVersionsInput) (*eks.DescribeClusterVersionsOutput, error) {
			failures++
			return nil, errors.New("denied")
		}}
	}
	for i := 0; i < 3; i++ {
		if result := failed.load(context.Background(), aws.Config{}, "us-east-1", "111111111111"); result.err == nil {
			t.Fatal("failure was replaced with empty successful catalog")
		}
	}
	if failures != 1 {
		t.Fatalf("failed requests = %d", failures)
	}
}

func TestEKSCatalogCanceledWaiterDoesNotCancelSharedLoad(t *testing.T) {
	catalog := NewEKSVersionCatalog()
	entered, release := make(chan struct{}), make(chan struct{})
	catalog.newClient = func(aws.Config) EKSAPI {
		return fakeEKSAPI{versions: func(context.Context, *eks.DescribeClusterVersionsInput) (*eks.DescribeClusterVersionsOutput, error) {
			close(entered)
			<-release
			return &eks.DescribeClusterVersionsOutput{}, nil
		}}
	}
	finished := make(chan eksCatalogResult, 1)
	go func() { finished <- catalog.load(context.Background(), aws.Config{}, "us-east-1", "111111111111") }()
	<-entered
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result := catalog.load(ctx, aws.Config{}, "us-east-1", "222222222222")
	close(release)
	first := <-finished
	if !errors.Is(result.err, context.Canceled) || first.err != nil {
		t.Fatalf("waiter=%v initial=%v", result.err, first.err)
	}
}

func TestRDSOnlyDoesNotLoadEKSCatalog(t *testing.T) {
	_, err := Fetch(context.Background(), Query{
		Targets: []AccountTarget{{AccountID: "111111111111", CatalogConfigLoader: func(context.Context) (aws.Config, error) {
			t.Error("RDS-only run loaded EKS credentials")
			return aws.Config{}, nil
		}}},
		Services: []Service{ServiceRDS}, regionLister: fakeRegionLister{regions: []string{"us-east-1"}}, rdsLister: fakeRDSLister{},
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestCombinedServicesPreserveFindingsWhenOneFails(t *testing.T) {
	for _, failed := range []Service{ServiceRDS, ServiceEKS} {
		t.Run(string(failed), func(t *testing.T) {
			catalog := NewEKSVersionCatalog()
			catalog.newClient = func(aws.Config) EKSAPI {
				return fakeEKSAPI{
					list: func(context.Context, *eks.ListClustersInput) (*eks.ListClustersOutput, error) {
						if failed == ServiceEKS {
							return nil, errors.New("EKS access denied")
						}
						return &eks.ListClustersOutput{Clusters: []string{"cluster"}}, nil
					},
					versions: func(context.Context, *eks.DescribeClusterVersionsInput) (*eks.DescribeClusterVersionsOutput, error) {
						return &eks.DescribeClusterVersionsOutput{ClusterVersions: []ekstypes.ClusterVersionInformation{testEKSVersion("1.31")}}, nil
					},
					describe: func(context.Context, *eks.DescribeClusterInput) (*eks.DescribeClusterOutput, error) {
						return &eks.DescribeClusterOutput{Cluster: &ekstypes.Cluster{Version: aws.String("1.31")}}, nil
					},
				}
			}
			rds := fakeRDSLister{records: []Record{{AccountID: "111111111111", Region: "us-east-1", Service: ServiceRDS, ResourceID: "database"}}}
			if failed == ServiceRDS {
				rds.err = errors.New("RDS partial discovery")
			}
			result, err := Fetch(context.Background(), Query{Targets: []AccountTarget{{AccountID: "111111111111"}}, Services: []Service{ServiceRDS, ServiceEKS}, Now: eksTestNow, EKSVersionCatalog: catalog, regionLister: fakeRegionLister{regions: []string{"us-east-1"}}, rdsLister: rds})
			if err != nil {
				t.Fatal(err)
			}
			want := 2
			if failed == ServiceEKS {
				want = 1
			}
			if len(result.Records) != want || result.Summary.TotalCount != want || len(result.Summary.SkippedRegions) != 1 || result.Summary.SkippedRegions[0].Service != failed || len(result.Summary.SkippedAccounts) != 0 {
				t.Fatalf("result = %+v", result)
			}
		})
	}
}
