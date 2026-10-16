package extendedsupport

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
)

type fakeRegionLister struct {
	regions []string
	err     error
}

func (f fakeRegionLister) ListEnabledRegions(
	_ context.Context, _ aws.Config, _ []string,
) ([]string, error) {
	return f.regions, f.err
}

type fakeRDSLister struct {
	records      []Record
	warnings     []RegionWarning
	unclassified []ResourceWarning
	err          error
}

func (f fakeRDSLister) ListRDSExtendedSupport(
	_ context.Context, _ aws.Config, region, accountID string, _ time.Time,
) (regionScanResult, error) {
	out := make([]Record, 0, len(f.records))
	for _, rec := range f.records {
		if rec.Region == region && rec.AccountID == accountID {
			out = append(out, rec)
		}
	}
	var warnings []RegionWarning
	for _, warning := range f.warnings {
		if warning.Region == region && warning.AccountID == accountID {
			warnings = append(warnings, warning)
		}
	}
	var unclassified []ResourceWarning
	for _, warning := range f.unclassified {
		if warning.Region == region && warning.AccountID == accountID {
			unclassified = append(unclassified, warning)
		}
	}
	return regionScanResult{Records: out, Warnings: warnings, UnclassifiedResources: unclassified}, f.err
}

func TestFetchAggregatesRecordsAndSummary(t *testing.T) {
	now := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
	records := []Record{
		{
			AccountID:    "111111111111",
			Region:       "us-east-1",
			Service:      ServiceRDS,
			ResourceType: ResourceTypeRDSInstance,
			ResourceID:   "db-a",
		},
		{
			AccountID:    "222222222222",
			Region:       "us-west-2",
			Service:      ServiceRDS,
			ResourceType: ResourceTypeRDSCluster,
			ResourceID:   "db-b",
		},
	}

	result, err := Fetch(context.Background(), Query{
		Targets: []AccountTarget{
			{AccountID: "111111111111"},
			{AccountID: "222222222222"},
		},
		Services:     []Service{ServiceRDS},
		Now:          now,
		regionLister: fakeRegionLister{regions: []string{"us-east-1", "us-west-2"}},
		rdsLister:    fakeRDSLister{records: records},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Records) != 2 {
		t.Fatalf("records = %d, want 2", len(result.Records))
	}
	if result.Records[0].AccountID != "111111111111" || result.Records[1].AccountID != "222222222222" {
		t.Fatalf("records not sorted by account: %+v", result.Records)
	}
	if result.Summary.TotalCount != 2 {
		t.Fatalf("total count = %d", result.Summary.TotalCount)
	}
	if len(result.Summary.ByAccount) != 2 || result.Summary.ByAccount[0].Count != 1 {
		t.Fatalf("by account = %+v", result.Summary.ByAccount)
	}
	if len(result.Summary.ByService) != 1 || result.Summary.ByService[0].Count != 2 {
		t.Fatalf("by service = %+v", result.Summary.ByService)
	}
}

func TestFetchRecordsRegionWarning(t *testing.T) {
	result, err := Fetch(context.Background(), Query{
		Targets:      []AccountTarget{{AccountID: "111111111111"}},
		Services:     []Service{ServiceRDS},
		regionLister: fakeRegionLister{regions: []string{"us-east-1"}},
		rdsLister:    fakeRDSLister{err: fmt.Errorf("describe db instances: connection refused")},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Records) != 0 {
		t.Fatalf("records = %d, want 0", len(result.Records))
	}
	if len(result.Summary.SkippedRegions) != 1 {
		t.Fatalf("skipped regions = %+v", result.Summary.SkippedRegions)
	}
	warning := result.Summary.SkippedRegions[0]
	if warning.AccountID != "111111111111" || warning.Region != "us-east-1" {
		t.Fatalf("warning = %+v", warning)
	}
	if warning.Message != "connection refused" {
		t.Fatalf("warning message = %q", warning.Message)
	}
}

func TestFetchSkipsAccountWithCredentialError(t *testing.T) {
	result, err := Fetch(context.Background(), Query{
		Targets: []AccountTarget{
			{
				AccountID: "111111111111",
				ConfigLoader: func(context.Context) (aws.Config, error) {
					return aws.Config{}, errors.New("ExpiredToken")
				},
			},
		},
		Services:     []Service{ServiceRDS},
		regionLister: fakeRegionLister{regions: []string{"us-east-1"}},
		rdsLister:    fakeRDSLister{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Records) != 0 {
		t.Fatalf("records = %d, want 0", len(result.Records))
	}
	if len(result.Summary.SkippedAccounts) != 1 {
		t.Fatalf("skipped accounts = %+v", result.Summary.SkippedAccounts)
	}
	if result.Summary.SkippedAccounts[0].Message != "ExpiredToken" {
		t.Fatalf("message = %q", result.Summary.SkippedAccounts[0].Message)
	}
}

func TestFetchRequiresTargets(t *testing.T) {
	_, err := Fetch(context.Background(), Query{Services: []Service{ServiceRDS}})
	if err == nil {
		t.Fatal("expected error for empty targets")
	}
}

func TestFetchSkippedAccountReasonsForBlankErrors(t *testing.T) {
	for _, stage := range []string{"credentials", "regions"} {
		for _, tc := range []struct {
			name    string
			message string
			want    string
		}{
			{name: "empty"},
			{name: "whitespace", message: " \t\r\n"},
			{name: "unicode whitespace", message: "\u00a0\u2003"},
			{name: "existing reason", message: "AccessDenied", want: "AccessDenied"},
			{name: "wrapped reason", message: "operation failed: permission denied", want: "permission denied"},
		} {
			t.Run(stage+"/"+tc.name, func(t *testing.T) {
				target := AccountTarget{AccountID: "111111111111", DisplayAlias: "member"}
				q := Query{
					Targets: []AccountTarget{target}, Services: []Service{ServiceRDS}, Workers: 1,
					regionLister: fakeRegionLister{regions: []string{"us-east-1"}},
					rdsLister:    fakeRDSLister{},
				}
				want := tc.want
				if stage == "credentials" {
					q.Targets[0].ConfigLoader = func(context.Context) (aws.Config, error) {
						return aws.Config{}, errors.New(tc.message)
					}
					if want == "" {
						want = "No error message was provided while loading account credentials"
					}
				} else {
					q.regionLister = fakeRegionLister{err: errors.New(tc.message)}
					if want == "" {
						want = "No error message was provided while discovering enabled regions"
					}
				}
				result, err := Fetch(context.Background(), q)
				if err != nil {
					t.Fatal(err)
				}
				if len(result.Summary.SkippedAccounts) != 1 {
					t.Fatalf("skipped accounts = %+v", result.Summary.SkippedAccounts)
				}
				warning := result.Summary.SkippedAccounts[0]
				if warning.AccountID != target.AccountID || warning.DisplayAlias != target.DisplayAlias || warning.Message != want {
					t.Fatalf("warning = %+v, want message %q", warning, want)
				}
			})
		}
	}
}

func TestFetchPreservesPartialResultsAndUnknownResources(t *testing.T) {
	const accountID = "111111111111"
	result, err := Fetch(context.Background(), Query{
		Targets: []AccountTarget{{AccountID: accountID}}, Services: []Service{ServiceRDS},
		regionLister: fakeRegionLister{regions: []string{"us-west-2", "us-east-1"}},
		rdsLister: fakeRDSLister{
			records:  []Record{{AccountID: accountID, Region: "us-east-1", Service: ServiceRDS, ResourceID: "db-known"}},
			warnings: []RegionWarning{{AccountID: accountID, Region: "us-east-1", Message: "DB cluster discovery incomplete"}},
			unclassified: []ResourceWarning{
				{AccountID: accountID, Region: "us-west-2", ResourceType: ResourceTypeRDSInstance, ResourceID: "db-west", Message: "engine version lookup failed"},
				{AccountID: accountID, Region: "us-east-1", ResourceType: ResourceTypeRDSInstance, ResourceID: "db-east", Message: "standard support end date is missing"},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Records) != 1 || result.Summary.TotalCount != 1 || result.Summary.ByAccount[0].Count != 1 {
		t.Fatalf("result = %+v", result)
	}
	if len(result.Summary.SkippedRegions) != 1 || len(result.Summary.UnclassifiedResources) != 2 || result.Summary.UnclassifiedResources[0].ResourceID != "db-east" {
		t.Fatalf("summary = %+v", result.Summary)
	}
}

func TestFetchCallsCredentialLoaderOncePerAccount(t *testing.T) {
	loads := 0
	result, err := Fetch(context.Background(), Query{
		Targets:  []AccountTarget{{AccountID: "111111111111", ConfigLoader: func(context.Context) (aws.Config, error) { loads++; return aws.Config{}, nil }}},
		Services: []Service{ServiceRDS}, Workers: 1,
		regionLister: fakeRegionLister{regions: []string{"us-east-1", "us-west-2"}},
		rdsLister:    fakeRDSLister{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if loads != 1 || len(result.Summary.SkippedAccounts) != 0 {
		t.Fatalf("loads = %d, summary = %+v", loads, result.Summary)
	}
}
