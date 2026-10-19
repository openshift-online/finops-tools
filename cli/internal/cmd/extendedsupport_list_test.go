package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/openshift-online/finops-tools/cli/internal/awsauth"
	"github.com/openshift-online/finops-tools/cli/internal/configstore"
	"github.com/openshift-online/finops-tools/cli/internal/output"
	"github.com/openshift-online/finops-tools/cli/internal/progress"
	"github.com/openshift-online/finops-tools/core/cost"
	"github.com/openshift-online/finops-tools/core/extendedsupport"
	"github.com/openshift-online/finops-tools/core/parallel"
	"github.com/spf13/cobra"
)

func TestExtendedSupportListPreRunRejectsUnknownService(t *testing.T) {
	extendedSupportListAccount = "123456789012"
	extendedSupportListAccountAliases = ""
	extendedSupportListFormat = string(output.FormatPrettyPrint)
	extendedSupportListServices = "elasticache"
	t.Cleanup(func() {
		extendedSupportListAccount = ""
		extendedSupportListServices = "rds"
	})

	cmd := extendedSupportListCmd
	cmd.SetOut(new(bytes.Buffer))
	cmd.SetErr(new(bytes.Buffer))
	if err := cmd.PreRunE(cmd, nil); err == nil {
		t.Fatal("expected error for unknown service")
	}
}

type fakeExtendedSupportPayer struct {
	config func(context.Context) (aws.Config, error)
	linked func(context.Context, string, string) (aws.Config, error)
}

func (p fakeExtendedSupportPayer) Config(ctx context.Context) (aws.Config, error) {
	if p.config != nil {
		return p.config(ctx)
	}
	return aws.Config{}, nil
}

func TestPrepareExtendedSupportTargetsKeepsPayersSeparate(t *testing.T) {
	cfg, path := testLinkedRoleConfig(t)
	cmd := testRoleFlagCmd()
	previous := newExtendedSupportPayerSession
	t.Cleanup(func() { newExtendedSupportPayerSession = previous })
	sessions := make(map[string]int)
	loaded := make(map[string]int)
	newExtendedSupportPayerSession = func(opts awsauth.EnsureOptions) extendedSupportPayerSession {
		sessions[opts.AccountName]++
		return fakeExtendedSupportPayer{
			config: func(context.Context) (aws.Config, error) { loaded[opts.AccountName]++; return aws.Config{}, nil },
			linked: func(context.Context, string, string) (aws.Config, error) {
				loaded[opts.AccountName]++
				return aws.Config{}, nil
			},
		}
	}
	targets, _, err := prepareExtendedSupportTargetsImpl(cmd, cfg, []cost.AccountTarget{
		{AccountID: "111111111111", PayerAccountID: "123456789012", DisplayAlias: "member"},
		{AccountID: "123456789012"},
		{AccountID: "222222222222", PayerAccountID: "987654321098"},
	}, "", path, "", 1, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, target := range targets {
		if _, err := target.ConfigLoader(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if len(sessions) != 2 || sessions["123456789012"] != 1 || sessions["987654321098"] != 1 || loaded["123456789012"] != 2 || loaded["987654321098"] != 1 {
		t.Fatalf("sessions=%v loaded=%v", sessions, loaded)
	}
}
func (p fakeExtendedSupportPayer) LinkedConfig(ctx context.Context, accountID, role string) (aws.Config, error) {
	return p.linked(ctx, accountID, role)
}

func TestPrepareExtendedSupportTargetsSharesPayerAndDefersLinkedAuthentication(t *testing.T) {
	cfg, path := testLinkedRoleConfig(t)
	cmd := testRoleFlagCmd()
	previous := newExtendedSupportPayerSession
	t.Cleanup(func() { newExtendedSupportPayerSession = previous })
	payerSessions, assumptions := 0, 0
	newExtendedSupportPayerSession = func(opts awsauth.EnsureOptions) extendedSupportPayerSession {
		payerSessions++
		if opts.AccountName != "123456789012" {
			t.Fatalf("payer = %q", opts.AccountName)
		}
		return fakeExtendedSupportPayer{linked: func(_ context.Context, accountID, role string) (aws.Config, error) {
			assumptions++
			if accountID == "222222222222" {
				return aws.Config{}, errors.New("AccessDenied")
			}
			if role != "arn:aws:iam::111111111111:role/OldRole" {
				t.Fatalf("role = %q", role)
			}
			return aws.Config{Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("LINKED", "SK", "ST")}, nil
		}}
	}
	targets, skipped, err := prepareExtendedSupportTargetsImpl(cmd, cfg, []cost.AccountTarget{
		{AccountID: "111111111111", PayerAccountID: "123456789012", DisplayAlias: "member"},
		{AccountID: "222222222222", PayerAccountID: "123456789012"},
	}, "", path, "", 1, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(targets) != 2 || len(skipped) != 0 || payerSessions != 1 || assumptions != 0 {
		t.Fatalf("targets=%v skipped=%v payerSessions=%d assumptions=%d", targets, skipped, payerSessions, assumptions)
	}
	for _, target := range targets {
		if target.CatalogAccountID != "123456789012" || target.CatalogConfigLoader == nil {
			t.Fatalf("payer catalog source not attached: %+v", target)
		}
	}
	awsCfg, err := targets[0].ConfigLoader(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	creds, err := awsCfg.Credentials.Retrieve(context.Background())
	if err != nil || creds.AccessKeyID != "LINKED" || assumptions != 1 {
		t.Fatalf("credentials retrieval failed: err=%v assumptions=%d", err, assumptions)
	}
	result, err := extendedsupport.Fetch(context.Background(), extendedsupport.Query{Targets: targets[1:], Services: []extendedsupport.Service{extendedsupport.ServiceRDS}, Workers: 1})
	if err != nil {
		t.Fatal(err)
	}
	if assumptions != 2 || len(result.Summary.SkippedAccounts) != 1 || result.Summary.SkippedAccounts[0].AccountID != "222222222222" {
		t.Fatalf("result=%+v assumptions=%d", result, assumptions)
	}
}

func TestPrepareExtendedSupportTargetsCreatesFreshPayerForRetry(t *testing.T) {
	cfg, path := testLinkedRoleConfig(t)
	cmd := testRoleFlagCmd()
	previous := newExtendedSupportPayerSession
	t.Cleanup(func() { newExtendedSupportPayerSession = previous })
	sessions := 0
	newExtendedSupportPayerSession = func(awsauth.EnsureOptions) extendedSupportPayerSession {
		sessions++
		generation := sessions
		return fakeExtendedSupportPayer{linked: func(context.Context, string, string) (aws.Config, error) {
			if generation == 1 {
				return aws.Config{}, errors.New("cached payer login failure")
			}
			return aws.Config{Region: "us-east-1"}, nil
		}}
	}
	for attempt := 1; attempt <= 2; attempt++ {
		targets, _, err := prepareExtendedSupportTargetsImpl(cmd, cfg, []cost.AccountTarget{
			{AccountID: "111111111111", PayerAccountID: "123456789012", DisplayAlias: "member"},
		}, "", path, "", 1, nil)
		if err != nil {
			t.Fatal(err)
		}
		_, err = targets[0].ConfigLoader(context.Background())
		if (attempt == 1 && err == nil) || (attempt == 2 && err != nil) {
			t.Fatalf("attempt %d: %v", attempt, err)
		}
	}
	if sessions != 2 {
		t.Fatalf("payer sessions = %d", sessions)
	}
}

func TestExtendedSupportListPreRunRejectsInvalidWorkers(t *testing.T) {
	extendedSupportListAccount = "123456789012"
	extendedSupportListFormat = string(output.FormatPrettyPrint)
	extendedSupportListServices = "rds"
	extendedSupportListWorkers = 0
	t.Cleanup(func() {
		extendedSupportListAccount = ""
		extendedSupportListWorkers = parallel.DefaultWorkers
	})

	cmd := extendedSupportListCmd
	cmd.SetOut(new(bytes.Buffer))
	cmd.SetErr(new(bytes.Buffer))
	if err := cmd.PreRunE(cmd, nil); err == nil {
		t.Fatal("expected error for workers < 1")
	}
}

func TestRunExtendedSupportListUsesFetchHook(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := configstore.RegisterAWSAccount(path, "123456789012", "rh-control"); err != nil {
		t.Fatal(err)
	}

	prevFetch := extendedSupportListFetch
	prevPrepare := prepareExtendedSupportTargets
	prevEnsure := ensureExtendedSupportCredentials
	t.Cleanup(func() {
		extendedSupportListFetch = prevFetch
		prepareExtendedSupportTargets = prevPrepare
		ensureExtendedSupportCredentials = prevEnsure
		awsFlags.ConfigPath = ""
	})

	extendedSupportListFetch = func(_ context.Context, q extendedsupport.Query) (extendedsupport.Result, error) {
		if len(q.Targets) != 1 || q.Targets[0].AccountID != "123456789012" {
			t.Fatalf("targets = %#v", q.Targets)
		}
		if len(q.Services) != 1 || q.Services[0] != extendedsupport.ServiceRDS {
			t.Fatalf("services = %#v", q.Services)
		}
		return extendedsupport.Result{
			Records: []extendedsupport.Record{{
				AccountID:    "123456789012",
				Region:       "us-east-1",
				Service:      extendedsupport.ServiceRDS,
				ResourceType: extendedsupport.ResourceTypeRDSInstance,
				ResourceID:   "db-old",
			}},
			Summary: extendedsupport.Summary{TotalCount: 1},
		}, nil
	}
	ensureExtendedSupportCredentials = func(_ *cobra.Command, _ configstore.File, _ []cost.AccountTarget, _, _, _ string) error {
		return nil
	}
	prepareExtendedSupportTargets = func(_ *cobra.Command, _ configstore.File, targets []cost.AccountTarget, _, _, _ string, _ int, _ *progress.Bar) ([]extendedsupport.AccountTarget, []extendedsupport.AccountWarning, error) {
		out := make([]extendedsupport.AccountTarget, 0, len(targets))
		for _, target := range targets {
			out = append(out, extendedsupport.AccountTarget{AccountID: target.AccountID})
		}
		return out, nil, nil
	}

	awsFlags.ConfigPath = path
	extendedSupportListAccount = ""
	extendedSupportListAccountAliases = "rh-control"
	extendedSupportListFormat = string(output.FormatJSON)
	extendedSupportListQuiet = true
	extendedSupportListServices = "rds"
	t.Cleanup(func() {
		extendedSupportListAccount = ""
		extendedSupportListAccountAliases = ""
		extendedSupportListFormat = string(output.FormatPrettyPrint)
		extendedSupportListQuiet = false
	})

	buf := new(bytes.Buffer)
	cmd := extendedSupportListCmd
	cmd.SetOut(buf)
	cmd.SetErr(new(bytes.Buffer))
	if err := runExtendedSupportList(cmd, nil); err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(buf.Bytes(), []byte(`"total_count": 1`)) {
		t.Fatalf("output = %s", buf.String())
	}
	if !bytes.Contains(buf.Bytes(), []byte(`"resource_id": "db-old"`)) {
		t.Fatalf("output = %s", buf.String())
	}
}

func TestRunExtendedSupportListRetriesSkippedAccountsOnce(t *testing.T) {
	for _, tc := range []struct {
		name                                                            string
		services                                                        string
		allSkipped, preparationSkipped, noSkipped, canceled, retryError bool
	}{
		{name: "some accounts skipped"},
		{name: "all accounts skipped", allSkipped: true},
		{name: "all accounts skipped during preparation", allSkipped: true, preparationSkipped: true},
		{name: "no skipped accounts", noSkipped: true},
		{name: "canceled scan", canceled: true},
		{name: "retry error", retryError: true},
		{name: "RDS and EKS share catalog through retry", services: "rds,eks"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.yaml")
			for alias, id := range map[string]string{"one": "111111111111", "two": "222222222222", "three": "333333333333"} {
				if err := configstore.RegisterAWSAccount(path, id, alias); err != nil {
					t.Fatal(err)
				}
			}
			prevFetch, prevRetry := extendedSupportListFetch, extendedSupportListRetry
			prevPrepare, prevEnsure := prepareExtendedSupportTargets, ensureExtendedSupportCredentials
			prevFlags := awsFlags
			t.Cleanup(func() {
				extendedSupportListFetch, extendedSupportListRetry = prevFetch, prevRetry
				prepareExtendedSupportTargets, ensureExtendedSupportCredentials = prevPrepare, prevEnsure
				awsFlags = prevFlags
				extendedSupportListAccountAliases = ""
				extendedSupportListFormat = string(output.FormatPrettyPrint)
				extendedSupportListServices = "rds"
				extendedSupportListRegions = ""
				extendedSupportListWorkers = parallel.DefaultWorkers
			})
			awsFlags.ConfigPath = path
			extendedSupportListAccountAliases = "one,two,three"
			extendedSupportListFormat = string(output.FormatJSON)
			extendedSupportListServices = "rds"
			if tc.services != "" {
				extendedSupportListServices = tc.services
			}
			extendedSupportListRegions = "us-east-1"
			extendedSupportListWorkers = 1
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			cmd := &cobra.Command{}
			cmd.SetContext(ctx)
			out, notices := new(bytes.Buffer), new(bytes.Buffer)
			cmd.SetOut(out)
			cmd.SetErr(notices)
			prepareCalls, fetchCalls, retryCalls := 0, 0, 0
			var scanTime time.Time
			var catalog *extendedsupport.EKSVersionCatalog
			ensureExtendedSupportCredentials = func(_ *cobra.Command, _ configstore.File, _ []cost.AccountTarget, _, _, _ string) error { return nil }
			prepareExtendedSupportTargets = func(_ *cobra.Command, _ configstore.File, targets []cost.AccountTarget, _, _, _ string, _ int, _ *progress.Bar) ([]extendedsupport.AccountTarget, []extendedsupport.AccountWarning, error) {
				prepareCalls++
				if prepareCalls > 2 {
					t.Fatal("more than one retry pass")
				}
				var prepared []extendedsupport.AccountTarget
				var skipped []extendedsupport.AccountWarning
				for _, target := range targets {
					if prepareCalls == 2 && !tc.allSkipped && target.AccountID == "111111111111" {
						t.Fatal("successful account was prepared again")
					}
					if tc.preparationSkipped && prepareCalls == 1 {
						skipped = append(skipped, extendedsupport.AccountWarning{AccountID: target.AccountID, Message: "preparation failed"})
						continue
					}
					prepared = append(prepared, extendedsupport.AccountTarget{AccountID: target.AccountID, AWSConfig: aws.Config{Region: map[bool]string{true: "fresh", false: "initial"}[prepareCalls == 2]}})
				}
				return prepared, skipped, nil
			}
			original := extendedsupport.Record{AccountID: "111111111111", Region: "us-east-1", Service: extendedsupport.ServiceRDS, ResourceID: "original"}
			extendedSupportListFetch = func(_ context.Context, q extendedsupport.Query) (extendedsupport.Result, error) {
				fetchCalls++
				scanTime = q.Now
				catalog = q.EKSVersionCatalog
				if tc.services != "" && catalog == nil {
					t.Fatal("combined scan has no shared EKS catalog")
				}
				if tc.canceled {
					cancel()
				}
				var result extendedsupport.Result
				for _, target := range q.Targets {
					if tc.noSkipped || (!tc.allSkipped && target.AccountID == original.AccountID) {
						result.Records = append(result.Records, extendedsupport.Record{AccountID: target.AccountID, ResourceID: "original", Service: extendedsupport.ServiceRDS})
						result.Summary.TotalCount++
					} else {
						result.Summary.SkippedAccounts = append(result.Summary.SkippedAccounts, extendedsupport.AccountWarning{AccountID: target.AccountID, Message: "initial failure"})
					}
				}
				return result, nil
			}
			retryFailure := errors.New("retry canceled")
			extendedSupportListRetry = func(_ context.Context, q extendedsupport.Query, result extendedsupport.Result) (extendedsupport.Result, error) {
				retryCalls++
				wantTargets := 2
				if tc.allSkipped {
					wantTargets = 3
				}
				wantServices := []extendedsupport.Service{extendedsupport.ServiceRDS}
				if tc.services != "" {
					wantServices = append(wantServices, extendedsupport.ServiceEKS)
				}
				if len(q.Targets) != wantTargets || len(result.Summary.SkippedAccounts) != wantTargets || q.Now.IsZero() || (!scanTime.IsZero() && !q.Now.Equal(scanTime)) || !reflect.DeepEqual(q.Regions, []string{"us-east-1"}) || !reflect.DeepEqual(q.Services, wantServices) || q.Workers != 1 || q.EKSVersionCatalog != catalog {
					t.Fatalf("retry query = %+v, initial result = %+v", q, result)
				}
				for _, target := range q.Targets {
					if target.AWSConfig.Region != "fresh" {
						t.Fatal("retry reused original configuration")
					}
				}
				if tc.retryError {
					return extendedsupport.Result{}, retryFailure
				}
				return extendedsupport.Result{
					Records: []extendedsupport.Record{original, {AccountID: "222222222222", ResourceID: "recovered"}},
					Summary: extendedsupport.Summary{TotalCount: 2, SkippedAccounts: []extendedsupport.AccountWarning{{AccountID: "333333333333", Message: "latest failure"}}},
				}, nil
			}
			err := runExtendedSupportList(cmd, nil)
			if tc.canceled {
				if !errors.Is(err, context.Canceled) || retryCalls != 0 || prepareCalls != 1 {
					t.Fatalf("error=%v retryCalls=%d prepareCalls=%d", err, retryCalls, prepareCalls)
				}
				return
			}
			if tc.retryError {
				if !errors.Is(err, retryFailure) || retryCalls != 1 {
					t.Fatalf("error=%v retryCalls=%d", err, retryCalls)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			var result extendedsupport.Result
			if err := json.Unmarshal(out.Bytes(), &result); err != nil {
				t.Fatalf("invalid JSON: %v: %s", err, out)
			}
			if tc.noSkipped {
				if retryCalls != 0 || prepareCalls != 1 || result.Summary.TotalCount != 3 {
					t.Fatalf("result=%+v retryCalls=%d prepareCalls=%d", result, retryCalls, prepareCalls)
				}
				return
			}
			wantFetchCalls := 1
			if tc.preparationSkipped {
				wantFetchCalls = 0
			}
			if prepareCalls != 2 || fetchCalls != wantFetchCalls || retryCalls != 1 || result.Summary.TotalCount != 2 || len(result.Records) != 2 || len(result.Summary.SkippedAccounts) != 1 || result.Summary.SkippedAccounts[0].Message != "latest failure" || !bytes.Contains(notices.Bytes(), []byte("Retrying")) {
				t.Fatalf("result=%+v prepareCalls=%d fetchCalls=%d retryCalls=%d notices=%s", result, prepareCalls, fetchCalls, retryCalls, notices)
			}
		})
	}
}

func TestMergeExtendedSupportSkippedAccountsKeepsLatestReason(t *testing.T) {
	result := mergeExtendedSupportSkippedAccounts(
		[]extendedsupport.AccountWarning{{AccountID: "222222222222", Message: "original"}, {AccountID: "111111111111", Message: "unchanged"}},
		[]extendedsupport.AccountWarning{{AccountID: "222222222222", Message: "latest"}},
	)
	want := []extendedsupport.AccountWarning{{AccountID: "111111111111", Message: "unchanged"}, {AccountID: "222222222222", Message: "latest"}}
	if !reflect.DeepEqual(result, want) {
		t.Fatalf("warnings = %+v", result)
	}
}
