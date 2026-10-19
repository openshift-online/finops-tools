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
)

type fakeEKSAPI struct {
	list     func(context.Context, *eks.ListClustersInput) (*eks.ListClustersOutput, error)
	describe func(context.Context, *eks.DescribeClusterInput) (*eks.DescribeClusterOutput, error)
	versions func(context.Context, *eks.DescribeClusterVersionsInput) (*eks.DescribeClusterVersionsOutput, error)
}

func (f fakeEKSAPI) ListClusters(ctx context.Context, input *eks.ListClustersInput, _ ...func(*eks.Options)) (*eks.ListClustersOutput, error) {
	if f.list == nil {
		return nil, errors.New("unexpected ListClusters")
	}
	return f.list(ctx, input)
}
func (f fakeEKSAPI) DescribeCluster(ctx context.Context, input *eks.DescribeClusterInput, _ ...func(*eks.Options)) (*eks.DescribeClusterOutput, error) {
	if f.describe == nil {
		return nil, errors.New("unexpected DescribeCluster")
	}
	return f.describe(ctx, input)
}
func (f fakeEKSAPI) DescribeClusterVersions(ctx context.Context, input *eks.DescribeClusterVersionsInput, _ ...func(*eks.Options)) (*eks.DescribeClusterVersionsOutput, error) {
	if f.versions == nil {
		return nil, errors.New("unexpected DescribeClusterVersions")
	}
	return f.versions(ctx, input)
}

var eksTestNow = time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)

func testEKSVersion(version string) ekstypes.ClusterVersionInformation {
	return ekstypes.ClusterVersionInformation{
		ClusterVersion:           aws.String(version),
		EndOfStandardSupportDate: aws.Time(eksTestNow.Add(-24 * time.Hour)),
		EndOfExtendedSupportDate: aws.Time(eksTestNow.Add(365 * 24 * time.Hour)),
		VersionStatus:            ekstypes.VersionStatusExtendedSupport,
	}
}

func TestEKSClassifiesClustersUsingVersionDates(t *testing.T) {
	for _, tc := range []struct {
		name    string
		change  func(*ekstypes.Cluster, *ekstypes.ClusterVersionInformation)
		record  bool
		warning string
	}{
		{name: "extended support", record: true},
		{name: "missing policy still uses dates", record: true, change: func(c *ekstypes.Cluster, _ *ekstypes.ClusterVersionInformation) { c.UpgradePolicy = nil }},
		{name: "standard policy still uses running version", record: true, change: func(c *ekstypes.Cluster, _ *ekstypes.ClusterVersionInformation) {
			c.UpgradePolicy.SupportType = ekstypes.SupportTypeStandard
		}},
		{name: "enabled extended policy on supported version", change: func(_ *ekstypes.Cluster, v *ekstypes.ClusterVersionInformation) {
			v.EndOfStandardSupportDate = aws.Time(eksTestNow.Add(time.Hour))
			v.VersionStatus = ekstypes.VersionStatusStandardSupport
		}},
		{name: "standard end boundary", record: true, change: func(_ *ekstypes.Cluster, v *ekstypes.ClusterVersionInformation) {
			v.EndOfStandardSupportDate = &eksTestNow
		}},
		{name: "extended end boundary", warning: "past extended support", change: func(_ *ekstypes.Cluster, v *ekstypes.ClusterVersionInformation) {
			v.EndOfExtendedSupportDate = &eksTestNow
		}},
		{name: "unsupported", warning: "past extended support", change: func(_ *ekstypes.Cluster, v *ekstypes.ClusterVersionInformation) {
			v.VersionStatus = ekstypes.VersionStatusUnsupported
		}},
		{name: "missing standard date", warning: "standard support end date is missing", change: func(_ *ekstypes.Cluster, v *ekstypes.ClusterVersionInformation) { v.EndOfStandardSupportDate = nil }},
		{name: "missing extended date", warning: "extended support end date is missing", change: func(_ *ekstypes.Cluster, v *ekstypes.ClusterVersionInformation) { v.EndOfExtendedSupportDate = nil }},
		{name: "zero extended date", warning: "extended support end date is missing", change: func(_ *ekstypes.Cluster, v *ekstypes.ClusterVersionInformation) {
			v.EndOfExtendedSupportDate = &time.Time{}
		}},
		{name: "inconsistent dates", warning: "dates are inconsistent", change: func(_ *ekstypes.Cluster, v *ekstypes.ClusterVersionInformation) {
			v.EndOfExtendedSupportDate = aws.Time(eksTestNow.Add(-48 * time.Hour))
		}},
		{name: "missing cluster version", warning: "Kubernetes version is missing", change: func(c *ekstypes.Cluster, _ *ekstypes.ClusterVersionInformation) { c.Version = nil }},
		{name: "missing catalog version", warning: "lifecycle information", change: func(c *ekstypes.Cluster, _ *ekstypes.ClusterVersionInformation) { c.Version = aws.String("1.99") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cluster := ekstypes.Cluster{Name: aws.String("test-cluster"), Version: aws.String("1.31"), Tags: map[string]string{"team": "finops"}, UpgradePolicy: &ekstypes.UpgradePolicyResponse{SupportType: ekstypes.SupportTypeExtended}}
			version := testEKSVersion("1.31")
			if tc.change != nil {
				tc.change(&cluster, &version)
			}
			catalogCalls := 0
			catalog := NewEKSVersionCatalog()
			catalog.newClient = func(cfg aws.Config) EKSAPI {
				if cfg.Region != "us-east-1" {
					t.Errorf("region = %q", cfg.Region)
				}
				return fakeEKSAPI{
					list: func(_ context.Context, input *eks.ListClustersInput) (*eks.ListClustersOutput, error) {
						if len(input.Include) != 0 {
							t.Error("external clusters requested")
						}
						return &eks.ListClustersOutput{Clusters: []string{"test-cluster"}}, nil
					},
					describe: func(_ context.Context, input *eks.DescribeClusterInput) (*eks.DescribeClusterOutput, error) {
						if aws.ToString(input.Name) != "test-cluster" {
							t.Errorf("name = %v", input.Name)
						}
						return &eks.DescribeClusterOutput{Cluster: &cluster}, nil
					},
					versions: func(_ context.Context, input *eks.DescribeClusterVersionsInput) (*eks.DescribeClusterVersionsOutput, error) {
						catalogCalls++
						if !aws.ToBool(input.IncludeAll) || input.DefaultOnly != nil || input.VersionStatus != "" {
							t.Errorf("catalog filters = %+v", input)
						}
						return &eks.DescribeClusterVersionsOutput{ClusterVersions: []ekstypes.ClusterVersionInformation{version}}, nil
					},
				}
			}
			result, err := Fetch(context.Background(), Query{Targets: []AccountTarget{{AccountID: "111111111111"}}, Services: []Service{ServiceEKS}, Now: eksTestNow, EKSVersionCatalog: catalog, regionLister: fakeRegionLister{regions: []string{"us-east-1"}}})
			if err != nil {
				t.Fatal(err)
			}
			wantRecords := 0
			if tc.record {
				wantRecords = 1
			}
			if catalogCalls != 1 || len(result.Records) != wantRecords || result.Summary.TotalCount != wantRecords {
				t.Fatalf("catalogCalls=%d result=%+v", catalogCalls, result)
			}
			if tc.warning != "" {
				if len(result.Summary.UnclassifiedResources) != 1 || !strings.Contains(result.Summary.UnclassifiedResources[0].Message, tc.warning) || result.Summary.UnclassifiedResources[0].Service != ServiceEKS {
					t.Fatalf("warnings = %+v", result.Summary)
				}
			} else if len(result.Summary.UnclassifiedResources) != 0 {
				t.Fatalf("unexpected warnings: %+v", result.Summary)
			}
			if tc.record {
				record := result.Records[0]
				cluster.Tags["team"] = "changed"
				if record.Service != ServiceEKS || record.ResourceType != ResourceTypeEKSCluster || record.Engine != "kubernetes" || record.EngineVersion != "1.31" || record.MajorEngineVersion != "1.31" || record.Tags["team"] != "finops" || !record.StandardSupportEndDate.Equal(*version.EndOfStandardSupportDate) {
					t.Fatalf("record = %+v", record)
				}
			}
		})
	}
}

func TestEKSPreservesPartialInventoryAndCatalog(t *testing.T) {
	catalog := NewEKSVersionCatalog()
	described := 0
	catalog.newClient = func(aws.Config) EKSAPI {
		return fakeEKSAPI{
			list: func(_ context.Context, input *eks.ListClustersInput) (*eks.ListClustersOutput, error) {
				if input.NextToken != nil {
					return nil, errors.New("list page unavailable")
				}
				return &eks.ListClustersOutput{Clusters: []string{"known", "unknown", "denied", "known", "", "another"}, NextToken: aws.String("next")}, nil
			},
			versions: func(_ context.Context, input *eks.DescribeClusterVersionsInput) (*eks.DescribeClusterVersionsOutput, error) {
				if input.NextToken != nil {
					return nil, errors.New("catalog page unavailable")
				}
				return &eks.DescribeClusterVersionsOutput{ClusterVersions: []ekstypes.ClusterVersionInformation{testEKSVersion("1.31")}, NextToken: aws.String("next")}, nil
			},
			describe: func(_ context.Context, input *eks.DescribeClusterInput) (*eks.DescribeClusterOutput, error) {
				described++
				name := aws.ToString(input.Name)
				if name == "denied" {
					return nil, errors.New("AccessDenied")
				}
				version := "1.31"
				if name == "unknown" {
					version = "1.30"
				}
				return &eks.DescribeClusterOutput{Cluster: &ekstypes.Cluster{Name: input.Name, Version: aws.String(version)}}, nil
			},
		}
	}
	result, err := Fetch(context.Background(), Query{Targets: []AccountTarget{{AccountID: "111111111111"}}, Services: []Service{ServiceEKS}, Now: eksTestNow, EKSVersionCatalog: catalog, regionLister: fakeRegionLister{regions: []string{"us-east-1"}}})
	if err != nil {
		t.Fatal(err)
	}
	if described != 4 || len(result.Records) != 2 || len(result.Summary.SkippedRegions) != 1 || len(result.Summary.UnclassifiedResources) != 2 {
		t.Fatalf("described=%d result=%+v", described, result)
	}
	for _, warning := range result.Summary.UnclassifiedResources {
		if warning.ResourceID == "unknown" && !strings.Contains(warning.Message, "catalog page unavailable") {
			t.Fatal(warning)
		}
		if warning.ResourceID == "denied" && !strings.Contains(warning.Message, "AccessDenied") {
			t.Fatal(warning)
		}
	}
}

func TestEKSHandlesEmptyAndMissingResponses(t *testing.T) {
	for _, stage := range []string{"empty", "list", "versions", "cluster"} {
		t.Run(stage, func(t *testing.T) {
			catalog := NewEKSVersionCatalog()
			calls := 0
			catalog.newClient = func(aws.Config) EKSAPI {
				return fakeEKSAPI{
					list: func(context.Context, *eks.ListClustersInput) (*eks.ListClustersOutput, error) {
						if stage == "list" {
							return nil, nil
						}
						if stage == "empty" {
							return &eks.ListClustersOutput{}, nil
						}
						return &eks.ListClustersOutput{Clusters: []string{"cluster"}}, nil
					},
					versions: func(context.Context, *eks.DescribeClusterVersionsInput) (*eks.DescribeClusterVersionsOutput, error) {
						calls++
						if stage == "versions" {
							return nil, nil
						}
						return &eks.DescribeClusterVersionsOutput{ClusterVersions: []ekstypes.ClusterVersionInformation{testEKSVersion("1.31")}}, nil
					},
					describe: func(context.Context, *eks.DescribeClusterInput) (*eks.DescribeClusterOutput, error) {
						if stage == "cluster" {
							return nil, nil
						}
						return &eks.DescribeClusterOutput{Cluster: &ekstypes.Cluster{Version: aws.String("1.31")}}, nil
					},
				}
			}
			result, err := (awsEKSLister{catalog: catalog}).ListEKSExtendedSupport(context.Background(), aws.Config{}, "us-east-1", "111111111111", eksTestNow)
			if err != nil {
				t.Fatal(err)
			}
			switch stage {
			case "empty":
				if calls != 0 || len(result.Records)+len(result.Warnings)+len(result.UnclassifiedResources) != 0 {
					t.Fatal(result)
				}
			case "list":
				if len(result.Warnings) != 1 {
					t.Fatal(result)
				}
			default:
				if len(result.UnclassifiedResources) != 1 {
					t.Fatal(result)
				}
			}
		})
	}
}

func TestEKSCancellationStopsDiscovery(t *testing.T) {
	for _, stage := range []string{"list", "versions", "describe"} {
		t.Run(stage, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			catalog := NewEKSVersionCatalog()
			catalog.newClient = func(aws.Config) EKSAPI {
				return fakeEKSAPI{
					list: func(context.Context, *eks.ListClustersInput) (*eks.ListClustersOutput, error) {
						if stage == "list" {
							cancel()
							return nil, ctx.Err()
						}
						return &eks.ListClustersOutput{Clusters: []string{"cluster"}}, nil
					},
					versions: func(context.Context, *eks.DescribeClusterVersionsInput) (*eks.DescribeClusterVersionsOutput, error) {
						if stage == "versions" {
							cancel()
							return nil, ctx.Err()
						}
						return &eks.DescribeClusterVersionsOutput{ClusterVersions: []ekstypes.ClusterVersionInformation{testEKSVersion("1.31")}}, nil
					},
					describe: func(context.Context, *eks.DescribeClusterInput) (*eks.DescribeClusterOutput, error) {
						cancel()
						return nil, ctx.Err()
					},
				}
			}
			_, err := Fetch(ctx, Query{Targets: []AccountTarget{{AccountID: "111111111111"}}, Services: []Service{ServiceEKS}, EKSVersionCatalog: catalog, regionLister: fakeRegionLister{regions: []string{"us-east-1"}}})
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestEKSPaginationContinuesThroughEmptyPages(t *testing.T) {
	pages := 0
	client := fakeEKSAPI{list: func(_ context.Context, input *eks.ListClustersInput) (*eks.ListClustersOutput, error) {
		pages++
		if input.NextToken == nil {
			return &eks.ListClustersOutput{NextToken: aws.String("next")}, nil
		}
		return &eks.ListClustersOutput{Clusters: []string{"cluster"}}, nil
	}}
	names, err := listEKSClusters(context.Background(), client)
	if err != nil || pages != 2 || fmt.Sprint(names) != "[cluster]" {
		t.Fatalf("names=%v pages=%d error=%v", names, pages, err)
	}
}

func TestEKSPaginationRejectsTokenCycles(t *testing.T) {
	for _, operation := range []string{"inventory", "catalog"} {
		t.Run(operation, func(t *testing.T) {
			pages := 0
			next := func() *string { pages++; return aws.String([]string{"one", "two", "one"}[pages-1]) }
			client := fakeEKSAPI{
				list: func(context.Context, *eks.ListClustersInput) (*eks.ListClustersOutput, error) {
					return &eks.ListClustersOutput{NextToken: next()}, nil
				},
				versions: func(context.Context, *eks.DescribeClusterVersionsInput) (*eks.DescribeClusterVersionsOutput, error) {
					return &eks.DescribeClusterVersionsOutput{NextToken: next()}, nil
				},
			}
			var err error
			if operation == "inventory" {
				_, err = listEKSClusters(context.Background(), client)
			} else {
				err = loadEKSVersions(context.Background(), client).err
			}
			if err == nil || !strings.Contains(err.Error(), "repeated pagination token") || pages != 3 {
				t.Fatalf("pages=%d error=%v", pages, err)
			}
		})
	}
}
