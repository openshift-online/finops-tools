package extendedsupport

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/eks"
)

type recordingSupportLister struct {
	mu    sync.Mutex
	calls []string
}

func (f *recordingSupportLister) ListRDSExtendedSupport(_ context.Context, _ aws.Config, region, accountID string, _ time.Time) (regionScanResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, region)
	return regionScanResult{Records: []Record{{AccountID: accountID, Region: region, Service: ServiceRDS, ResourceID: "db"}}}, nil
}

func TestFetchAndRetryExcludeRegions(t *testing.T) {
	for _, tc := range []struct {
		name    string
		regions []string
		want    int
	}{
		{name: "discovered", want: 1},
		{name: "explicit", regions: []string{"us-east-1", "us-west-2"}, want: 1},
		{name: "all excluded", regions: []string{"us-west-2"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			lister := &recordingSupportLister{}
			loads := 0
			q := Query{
				Targets: []AccountTarget{{AccountID: "111111111111", ConfigLoader: func(context.Context) (aws.Config, error) {
					loads++
					if loads == 1 {
						return aws.Config{}, errors.New("temporary credentials failure")
					}
					return aws.Config{}, nil
				}}},
				Services: []Service{ServiceRDS}, Regions: tc.regions, SkipRegions: []string{" us-west-2 ", "us-west-2"},
				regionLister: selectedSupportRegions{}, rdsLister: lister,
			}
			initial, err := Fetch(context.Background(), q)
			if err != nil || len(initial.Summary.SkippedAccounts) != 1 {
				t.Fatalf("initial=%+v err=%v", initial, err)
			}
			result, err := RetrySkippedAccounts(context.Background(), q, initial)
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Records) != tc.want || len(lister.calls) != tc.want || len(result.Summary.SkippedAccounts) != 0 || len(result.Summary.SkippedRegions) != 0 {
				t.Fatalf("result=%+v calls=%v", result, lister.calls)
			}
			for _, region := range lister.calls {
				if region != "us-east-1" {
					t.Fatalf("called excluded region %s", region)
				}
			}
			if !reflect.DeepEqual(result.Summary.ExcludedRegions, []string{"us-west-2"}) {
				t.Fatalf("exclusions lost during retry: %+v", result.Summary)
			}
		})
	}
}

type selectedSupportRegions struct{}

func (selectedSupportRegions) ListEnabledRegions(_ context.Context, _ aws.Config, requested []string) ([]string, error) {
	if len(requested) > 0 {
		return requested, nil
	}
	return []string{"us-east-1", "us-west-2"}, nil
}

func TestEKSCatalogPreloadExcludesRegions(t *testing.T) {
	for _, all := range []bool{false, true} {
		catalog := NewEKSVersionCatalog()
		var calls []string
		catalog.newClient = func(cfg aws.Config) EKSAPI {
			return fakeEKSAPI{versions: func(context.Context, *eks.DescribeClusterVersionsInput) (*eks.DescribeClusterVersionsOutput, error) {
				calls = append(calls, cfg.Region)
				return &eks.DescribeClusterVersionsOutput{}, nil
			}}
		}
		q := Query{
			Targets:     []AccountTarget{{AccountID: "111111111111", CatalogConfigLoader: func(context.Context) (aws.Config, error) { return aws.Config{}, nil }}},
			SkipRegions: []string{"us-west-2"}, regionLister: selectedSupportRegions{},
		}
		if all {
			q.Regions = []string{"us-west-2"}
		}
		if err := catalog.preload(context.Background(), q); err != nil {
			t.Fatal(err)
		}
		if (!all && !reflect.DeepEqual(calls, []string{"us-east-1"})) || (all && len(calls) != 0) {
			t.Fatalf("all excluded=%v, catalog calls=%v", all, calls)
		}
	}
}
