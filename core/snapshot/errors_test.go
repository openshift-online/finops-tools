package snapshot

import (
	"context"
	"errors"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
)

type flakyEBSLister struct {
	failRegions map[string]error
	records     []Record
}

func (f flakyEBSLister) ListEBSSnapshots(
	_ context.Context,
	_ aws.Config,
	region, accountID string,
	_ time.Time,
	_ float64,
) ([]Record, float64, error) {
	if err, ok := f.failRegions[region]; ok {
		return nil, 0, err
	}
	out := make([]Record, 0, len(f.records))
	for _, rec := range f.records {
		if rec.Region == region && rec.AccountID == accountID {
			out = append(out, rec)
		}
	}
	return out, 0, nil
}

func TestFetchContinuesWhenRegionFails(t *testing.T) {
	timeoutErr := fmt.Errorf(`describe ebs snapshots in me-south-1: operation error EC2: DescribeSnapshots, exceeded maximum number of attempts, 3, request send failed, Post "https://ec2.me-south-1.amazonaws.com/": dial tcp 99.82.136.87:443: i/o timeout`)
	ebs := Record{
		AccountID:               "111111111111",
		Region:                  "us-east-1",
		Kind:                    KindEBSSnapshot,
		ResourceID:              "snap-old",
		EstimatedMonthlyCostUSD: 5,
	}

	result, err := Fetch(context.Background(), Query{
		Targets:   []AccountTarget{{AccountID: "111111111111"}},
		OlderThan: 180 * 24 * time.Hour,
		Types:     []Kind{KindEBSSnapshot},
		regionLister: fakeRegionLister{regions: []string{"us-east-1", "me-south-1"}},
		ebsLister: flakyEBSLister{
			failRegions: map[string]error{"me-south-1": timeoutErr},
			records:     []Record{ebs},
		},
		rdsLister: fakeRDSLister{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Records) != 1 {
		t.Fatalf("records = %d, want 1", len(result.Records))
	}
	if len(result.Summary.SkippedRegions) != 1 {
		t.Fatalf("skipped = %#v", result.Summary.SkippedRegions)
	}
	if result.Summary.SkippedRegions[0].Region != "me-south-1" {
		t.Fatalf("skipped region = %q", result.Summary.SkippedRegions[0].Region)
	}
}

func TestFetchSkipsAccountWhenAllRegionsFail(t *testing.T) {
	errDenied := errors.New("ec2:DescribeSnapshots because no identity-based policy allows the ec2:DescribeSnapshots action")
	result, err := Fetch(context.Background(), Query{
		Targets:   []AccountTarget{{AccountID: "111111111111"}},
		OlderThan: 180 * 24 * time.Hour,
		Types:     []Kind{KindEBSSnapshot},
		regionLister: fakeRegionLister{regions: []string{"us-east-1", "us-west-2"}},
		ebsLister: flakyEBSLister{
			failRegions: map[string]error{
				"us-east-1": errDenied,
				"us-west-2": errDenied,
			},
		},
		rdsLister: fakeRDSLister{},
	})
	if err != nil {
		t.Fatalf("Fetch() error = %v, want nil so other accounts can continue", err)
	}
	if len(result.Records) != 0 {
		t.Fatalf("records = %d, want 0", len(result.Records))
	}
	if len(result.Summary.SkippedRegions) != 1 {
		t.Fatalf("skipped = %#v, want one collapsed warning", result.Summary.SkippedRegions)
	}
	if result.Summary.SkippedRegions[0].Region != "all" {
		t.Fatalf("region = %q, want all", result.Summary.SkippedRegions[0].Region)
	}
}

func TestCollapseRegionWarnings(t *testing.T) {
	warnings := []RegionWarning{
		{AccountID: "111111111111", Region: "us-east-1", Message: "access denied"},
		{AccountID: "111111111111", Region: "us-west-2", Message: "access denied"},
	}
	got := collapseRegionWarnings("111111111111", []string{"us-east-1", "us-west-2"}, warnings)
	if len(got) != 1 || got[0].Region != "all" {
		t.Fatalf("got %#v", got)
	}

	mixed := []RegionWarning{
		{AccountID: "111111111111", Region: "us-east-1", Message: "access denied"},
		{AccountID: "111111111111", Region: "us-west-2", Message: "timeout"},
	}
	if len(collapseRegionWarnings("111111111111", []string{"us-east-1", "us-west-2"}, mixed)) != 2 {
		t.Fatal("expected distinct warnings to remain separate")
	}
}

func TestRegionErrorMessage(t *testing.T) {
	err := fmt.Errorf(`describe ebs snapshots in me-south-1: dial tcp: i/o timeout`)
	got := regionErrorMessage(err)
	if got != "i/o timeout" {
		t.Fatalf("message = %q", got)
	}
}

func TestIsExpiredCredentialError(t *testing.T) {
	if !isExpiredCredentialError(errors.New("Request has expired.")) {
		t.Fatal("expected expired request")
	}
	if !isExpiredCredentialError(errors.New("api error AuthFailure: AWS was not able to validate the provided access credentials")) {
		t.Fatal("expected AuthFailure to match")
	}
	if isExpiredCredentialError(errors.New("access denied")) {
		t.Fatal("expected access denied not to match")
	}
}

func TestFetchRefreshesExpiredCredentials(t *testing.T) {
	calls := 0
	loader := func(context.Context) (aws.Config, error) {
		calls++
		return aws.Config{}, nil
	}
	ebs := &expiringEBSLister{
		record: Record{
			AccountID:  "111111111111",
			Region:     "us-east-1",
			Kind:       KindEBSSnapshot,
			ResourceID: "snap-old",
		},
	}

	result, err := Fetch(context.Background(), Query{
		Targets: []AccountTarget{{
			AccountID:    "111111111111",
			ConfigLoader: loader,
		}},
		OlderThan:    180 * 24 * time.Hour,
		Types:        []Kind{KindEBSSnapshot},
		regionLister: fakeRegionLister{regions: []string{"us-east-1"}},
		ebsLister:    ebs,
		rdsLister:    fakeRDSLister{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Records) != 1 {
		t.Fatalf("records = %d, want 1", len(result.Records))
	}
	if calls < 2 {
		t.Fatalf("ConfigLoader calls = %d, want at least 2 (scan + refresh)", calls)
	}
}

func TestFetchRefreshesCredentialsOnDescribeRegionsAuthFailure(t *testing.T) {
	loaderCalls := 0
	loader := func(context.Context) (aws.Config, error) {
		loaderCalls++
		return aws.Config{}, nil
	}
	lister := &authFailThenOKRegionLister{
		regions: []string{"us-east-1"},
	}
	ebs := Record{
		AccountID:  "111111111111",
		Region:     "us-east-1",
		Kind:       KindEBSSnapshot,
		ResourceID: "snap-old",
	}

	result, err := Fetch(context.Background(), Query{
		Targets: []AccountTarget{{
			AccountID:    "111111111111",
			ConfigLoader: loader,
		}},
		OlderThan:    180 * 24 * time.Hour,
		Types:        []Kind{KindEBSSnapshot},
		regionLister: lister,
		ebsLister:    fakeEBSLister{records: []Record{ebs}},
		rdsLister:    fakeRDSLister{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Records) != 1 {
		t.Fatalf("records = %d, want 1", len(result.Records))
	}
	if lister.calls != 2 {
		t.Fatalf("ListEnabledRegions calls = %d, want 2", lister.calls)
	}
	if loaderCalls < 2 {
		t.Fatalf("ConfigLoader calls = %d, want at least 2", loaderCalls)
	}
}

func TestFetchSkipsAccountWhenDescribeRegionsFails(t *testing.T) {
	authErr := errors.New("api error AuthFailure: AWS was not able to validate the provided access credentials")
	okEBS := Record{
		AccountID:               "111111111111",
		Region:                  "us-east-1",
		Kind:                    KindEBSSnapshot,
		ResourceID:              "snap-ok",
		EstimatedMonthlyCostUSD: 5,
	}

	result, err := Fetch(context.Background(), Query{
		Targets: []AccountTarget{
			{
				AccountID:    "999999999999",
				DisplayAlias: "bad-acct",
				AWSConfig:    aws.Config{Region: "bad"},
				// Refresh still returns bad creds; account should be skipped, not abort Fetch.
				ConfigLoader: func(context.Context) (aws.Config, error) {
					return aws.Config{Region: "bad"}, nil
				},
			},
			{AccountID: "111111111111", AWSConfig: aws.Config{Region: "ok"}},
		},
		OlderThan: 180 * 24 * time.Hour,
		Types:     []Kind{KindEBSSnapshot},
		Workers:   1,
		regionLister: &keyedFailRegionLister{
			failKey: "bad",
			failErr: authErr,
			okByKey: map[string][]string{
				"ok": {"us-east-1"},
			},
		},
		ebsLister: fakeEBSLister{records: []Record{okEBS}},
		rdsLister: fakeRDSLister{},
	})
	if err != nil {
		t.Fatalf("Fetch() error = %v, want nil so other accounts continue", err)
	}
	if len(result.Records) != 1 || result.Records[0].ResourceID != "snap-ok" {
		t.Fatalf("records = %#v", result.Records)
	}
	if len(result.Summary.SkippedAccounts) != 1 {
		t.Fatalf("skipped = %#v", result.Summary.SkippedAccounts)
	}
	if result.Summary.SkippedAccounts[0].AccountID != "999999999999" {
		t.Fatalf("skipped account = %#v", result.Summary.SkippedAccounts[0])
	}
	if result.Summary.SkippedAccounts[0].DisplayAlias != "bad-acct" {
		t.Fatalf("alias = %q", result.Summary.SkippedAccounts[0].DisplayAlias)
	}
}

type authFailThenOKRegionLister struct {
	calls   int
	regions []string
}

func (a *authFailThenOKRegionLister) ListEnabledRegions(_ context.Context, _ aws.Config, _ []string) ([]string, error) {
	a.calls++
	if a.calls == 1 {
		return nil, errors.New("api error AuthFailure: AWS was not able to validate the provided access credentials")
	}
	return a.regions, nil
}

// keyedFailRegionLister fails when cfg.Region matches failKey (used to target one account).
type keyedFailRegionLister struct {
	failKey string
	failErr error
	okByKey map[string][]string
}

func (k *keyedFailRegionLister) ListEnabledRegions(_ context.Context, cfg aws.Config, _ []string) ([]string, error) {
	if cfg.Region == k.failKey {
		return nil, k.failErr
	}
	if regions, ok := k.okByKey[cfg.Region]; ok {
		return regions, nil
	}
	return k.okByKey[""], nil
}

type expiringEBSLister struct {
	calls  int
	record Record
}

func (e *expiringEBSLister) ListEBSSnapshots(
	_ context.Context,
	_ aws.Config,
	region, accountID string,
	_ time.Time,
	_ float64,
) ([]Record, float64, error) {
	e.calls++
	if e.calls == 1 {
		return nil, 0, errors.New("Request has expired.")
	}
	if region == e.record.Region && accountID == e.record.AccountID {
		return []Record{e.record}, 0, nil
	}
	return nil, 0, nil
}

func TestIsSkippableRegionError(t *testing.T) {
	var timeout net.Error = fakeTimeoutError{}
	if !isSkippableRegionError(timeout) {
		t.Fatal("expected timeout to be skippable")
	}
	if isSkippableRegionError(context.Canceled) {
		t.Fatal("expected context.Canceled not to be skippable")
	}
}

type fakeTimeoutError struct{}

func (fakeTimeoutError) Error() string   { return "timeout" }
func (fakeTimeoutError) Timeout() bool   { return true }
func (fakeTimeoutError) Temporary() bool { return true }
