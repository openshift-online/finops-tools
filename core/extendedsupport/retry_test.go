package extendedsupport

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
)

type retryRegionLister struct {
	list func(context.Context, aws.Config) ([]string, error)
}

func (f retryRegionLister) ListEnabledRegions(ctx context.Context, cfg aws.Config, _ []string) ([]string, error) {
	return f.list(ctx, cfg)
}

func TestRetrySkippedAccountsAfterInitialScan(t *testing.T) {
	const (
		success            = "111111111111"
		recoverCredentials = "222222222222"
		recoverRegions     = "333333333333"
		failCredentials    = "999999999999"
		failRegions        = "987654321098"
		missingTarget      = "123456789012"
	)
	loads := make(map[string]int)
	regionCalls := make(map[string]int)
	q := Query{
		Services: []Service{ServiceRDS},
		Workers:  1,
		Now:      time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC),
		regionLister: retryRegionLister{list: func(_ context.Context, cfg aws.Config) ([]string, error) {
			id := cfg.Region
			regionCalls[id]++
			if (id == recoverRegions && regionCalls[id] == 1) || id == failRegions {
				if regionCalls[id] == 1 {
					return nil, errors.New("initial region discovery failed")
				}
				return nil, errors.New("retry region discovery failed")
			}
			return []string{"us-east-1"}, nil
		}},
		rdsLister: fakeRDSLister{
			records: []Record{
				{AccountID: success, Region: "us-east-1", Service: ServiceRDS, ResourceID: "original"},
				{AccountID: recoverCredentials, Region: "us-east-1", Service: ServiceRDS, ResourceID: "recovered"},
			},
			warnings: []RegionWarning{{AccountID: success, Region: "us-east-1", Message: "partial discovery"}},
			unclassified: []ResourceWarning{
				{AccountID: success, Region: "us-east-1", ResourceID: "unknown-original"},
				{AccountID: recoverCredentials, Region: "us-east-1", ResourceID: "unknown-recovered"},
			},
		},
	}
	for _, id := range []string{success, recoverCredentials, recoverRegions, failCredentials, failRegions} {
		q.Targets = append(q.Targets, AccountTarget{AccountID: id, ConfigLoader: func(context.Context) (aws.Config, error) {
			loads[id]++
			if (id == recoverCredentials && loads[id] == 1) || id == failCredentials {
				if loads[id] == 1 {
					return aws.Config{}, errors.New("initial credentials unavailable")
				}
				return aws.Config{}, errors.New("retry credentials unavailable")
			}
			return aws.Config{Region: id}, nil
		}})
	}
	initial, err := Fetch(context.Background(), q)
	if err != nil {
		t.Fatal(err)
	}
	if len(initial.Summary.SkippedAccounts) != 4 {
		t.Fatalf("initial summary = %+v", initial.Summary)
	}
	initial.Summary.SkippedAccounts = append(initial.Summary.SkippedAccounts, AccountWarning{AccountID: missingTarget, Message: "no retry target"})
	before, _ := json.Marshal(initial)
	// A repeated target must still be attempted only once in the retry pass.
	q.Targets = append(q.Targets, q.Targets[1])
	result, err := RetrySkippedAccounts(context.Background(), q, initial)
	if err != nil {
		t.Fatal(err)
	}
	if loads[success] != 1 || loads[recoverCredentials] != 2 || loads[recoverRegions] != 2 || loads[failCredentials] != 2 || loads[failRegions] != 2 {
		t.Fatalf("account attempts = %v", loads)
	}
	if len(result.Records) != 2 || result.Summary.TotalCount != 2 || len(result.Summary.ByAccount) != 2 || len(result.Summary.ByService) != 1 || result.Summary.ByService[0].Count != 2 {
		t.Fatalf("result = %+v", result)
	}
	if result.Records[0].ResourceID != "original" || result.Records[1].ResourceID != "recovered" {
		t.Fatalf("records = %+v", result.Records)
	}
	wantSkipped := []AccountWarning{
		{AccountID: missingTarget, Message: "no retry target"},
		{AccountID: failRegions, Message: "retry region discovery failed"},
		{AccountID: failCredentials, Message: "retry credentials unavailable"},
	}
	if !reflect.DeepEqual(result.Summary.SkippedAccounts, wantSkipped) {
		t.Fatalf("skipped accounts = %+v", result.Summary.SkippedAccounts)
	}
	if len(result.Summary.SkippedRegions) != 1 || result.Summary.SkippedRegions[0].AccountID != success || len(result.Summary.UnclassifiedResources) != 2 {
		t.Fatalf("warnings = %+v", result.Summary)
	}
	after, _ := json.Marshal(initial)
	if string(before) != string(after) {
		t.Fatal("retry mutated the original result")
	}
}

func TestRetrySkippedAccountsCanBecomeRegionWarnings(t *testing.T) {
	initial := Result{Summary: Summary{SkippedAccounts: []AccountWarning{{AccountID: "111111111111", Message: "credentials unavailable"}}}}
	result, err := RetrySkippedAccounts(context.Background(), Query{
		Targets:      []AccountTarget{{AccountID: "111111111111"}},
		Services:     []Service{ServiceRDS},
		regionLister: fakeRegionLister{regions: []string{"us-east-1"}},
		rdsLister:    fakeRDSLister{err: errors.New("RDS access denied")},
	}, initial)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Summary.SkippedAccounts) != 0 || len(result.Summary.SkippedRegions) != 1 || result.Summary.SkippedRegions[0].Message != "RDS access denied" {
		t.Fatalf("summary = %+v", result.Summary)
	}
}

func TestRetrySkippedAccountsDoesNothingWithoutMatchingTargets(t *testing.T) {
	for _, skipped := range [][]AccountWarning{nil, {{AccountID: "222222222222", Message: "unavailable"}}} {
		initial := Result{Summary: Summary{SkippedAccounts: skipped, SkippedRegions: []RegionWarning{{AccountID: "111111111111", Region: "us-east-1"}}}}
		result, err := RetrySkippedAccounts(context.Background(), Query{Targets: []AccountTarget{{AccountID: "111111111111", ConfigLoader: func(context.Context) (aws.Config, error) {
			t.Fatal("an account outside the skipped list was retried")
			return aws.Config{}, nil
		}}}}, initial)
		if err != nil || !reflect.DeepEqual(result, initial) {
			t.Fatalf("result = %+v, error = %v", result, err)
		}
	}
}

func TestRetrySkippedAccountsHonorsCancellation(t *testing.T) {
	for _, cancelBefore := range []bool{true, false} {
		t.Run(map[bool]string{true: "before retry", false: "during retry"}[cancelBefore], func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if cancelBefore {
				cancel()
			}
			initial := Result{Summary: Summary{SkippedAccounts: []AccountWarning{{AccountID: "111111111111", Message: "unavailable"}}}}
			result, err := RetrySkippedAccounts(ctx, Query{
				Services: []Service{ServiceRDS},
				Targets: []AccountTarget{{AccountID: "111111111111", ConfigLoader: func(context.Context) (aws.Config, error) {
					cancel()
					return aws.Config{}, ctx.Err()
				}}},
			}, initial)
			if !errors.Is(err, context.Canceled) || !reflect.DeepEqual(result, initial) {
				t.Fatalf("result = %+v, error = %v", result, err)
			}
		})
	}
}
