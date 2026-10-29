package snapshot

import (
	"context"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
)

type selectedRegionLister struct{}

func (selectedRegionLister) ListEnabledRegions(_ context.Context, cfg aws.Config, requested []string) ([]string, error) {
	if len(requested) > 0 {
		return requested, nil
	}
	return []string{cfg.Region, "us-west-2"}, nil
}

type recordingSnapshotLister struct {
	mu    sync.Mutex
	calls []string
}

func (f *recordingSnapshotLister) record(service, region, accountID string) []Record {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, service+":"+region)
	return []Record{{AccountID: accountID, Region: region, ResourceID: service, Kind: KindEBSSnapshot}}
}

func (f *recordingSnapshotLister) ListEBSSnapshots(_ context.Context, _ aws.Config, region, accountID string, _ time.Time, _ float64) ([]Record, float64, error) {
	return f.record("ebs", region, accountID), 0, nil
}

func (f *recordingSnapshotLister) ListRDSSnapshots(_ context.Context, _ aws.Config, region, accountID string, _ time.Time, _ float64) ([]Record, error) {
	records := f.record("rds", region, accountID)
	records[0].Kind = KindRDSSnapshot
	return records, nil
}

func (f *recordingSnapshotLister) GetRDSRegionContext(_ context.Context, _ aws.Config, region string) (RDSRegionContext, error) {
	f.record("rds-context", region, "")
	return RDSRegionContext{}, nil
}

func TestFetchExcludesRegionsBeforeSnapshotCalls(t *testing.T) {
	for _, tc := range []struct {
		name    string
		regions []string
		skip    []string
		want    int
	}{
		{name: "discovered per account", skip: []string{" us-west-2 ", "us-west-2"}, want: 4},
		{name: "explicit inclusion", regions: []string{"us-east-1", "us-west-2"}, skip: []string{"us-west-2"}, want: 4},
		{name: "all excluded", regions: []string{"us-west-2"}, skip: []string{"us-west-2"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			lister := &recordingSnapshotLister{}
			result, err := Fetch(context.Background(), Query{
				Targets: []AccountTarget{
					{AccountID: "111111111111", AWSConfig: aws.Config{Region: "us-east-1"}},
					{AccountID: "222222222222", AWSConfig: aws.Config{Region: "eu-west-1"}},
				},
				Types: []Kind{KindEBSSnapshot, KindRDSSnapshot}, OlderThan: 24 * time.Hour,
				Regions: tc.regions, SkipRegions: tc.skip, regionLister: selectedRegionLister{},
				ebsLister: lister, rdsLister: lister,
			})
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Records) != tc.want || len(lister.calls) != tc.want/2*3 {
				t.Fatalf("records=%v, calls=%v", result.Records, lister.calls)
			}
			for _, call := range lister.calls {
				if call == "ebs:us-west-2" || call == "rds:us-west-2" || call == "rds-context:us-west-2" {
					t.Fatalf("excluded region was called: %s", call)
				}
			}
			if !reflect.DeepEqual(result.Summary.ExcludedRegions, []string{"us-west-2"}) || len(result.Summary.SkippedRegions) != 0 {
				t.Fatalf("summary = %+v", result.Summary)
			}
			if tc.name == "discovered per account" {
				found := false
				for _, rec := range result.Records {
					found = found || rec.Region == "eu-west-1"
				}
				if !found {
					t.Fatalf("member-specific enabled region was lost: %v", result.Records)
				}
			}
		})
	}
}
