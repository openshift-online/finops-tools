package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/openshift-online/finops-tools/cli/internal/configstore"
	"github.com/openshift-online/finops-tools/cli/internal/output"
	"github.com/openshift-online/finops-tools/cli/internal/progress"
	coreaccount "github.com/openshift-online/finops-tools/core/account"
	"github.com/openshift-online/finops-tools/core/cost"
	"github.com/openshift-online/finops-tools/core/extendedsupport"
	"github.com/spf13/cobra"
)

func TestFilterExtendedSupportBillingTargets(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := configstore.RegisterAWSAccount(path, "123456789012", "payer"); err != nil {
		t.Fatal(err)
	}
	cfg, err := configstore.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	targets := []cost.AccountTarget{
		{AccountID: "111111111111", PayerAccountID: "123456789012"},
		{AccountID: "222222222222", PayerAccountID: "123456789012", DisplayName: "Member Two", DisplayAlias: "two"},
	}
	for _, tc := range []struct {
		name                                 string
		sel                                  costTargetSelector
		fullScan, empty, failed, invalidDays bool
		want                                 int
	}{
		{name: "payer filters", sel: costTargetSelector{PayerAliases: []string{"payer"}}, want: 1},
		{name: "empty billed selection", sel: costTargetSelector{PayerAliases: []string{"payer"}}, empty: true},
		{name: "full scan bypass", sel: costTargetSelector{PayerAliases: []string{"payer"}}, fullScan: true, want: 2},
		{name: "explicit bypass", sel: costTargetSelector{AccountIDs: []string{"111111111111"}, PayerAliases: []string{"payer"}}, want: 2},
		{name: "OU bypass", sel: costTargetSelector{OUs: []configstore.OUSelector{{ID: "ou-abcd-12345678"}}, PayerAliases: []string{"payer"}}, want: 2},
		{name: "tag bypass", sel: costTargetSelector{TagKey: "team", PayerAliases: []string{"payer"}}, want: 2},
		{name: "API failure", sel: costTargetSelector{PayerAliases: []string{"payer"}}, failed: true},
		{name: "invalid lookback", sel: costTargetSelector{PayerAliases: []string{"payer"}}, invalidDays: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			previousLoad, previousDiscover := loadAWSConfigForCredentialsAccount, discoverExtendedSupportBillingCandidates
			t.Cleanup(func() {
				loadAWSConfigForCredentialsAccount, discoverExtendedSupportBillingCandidates = previousLoad, previousDiscover
			})
			loads, queries := 0, 0
			loadAWSConfigForCredentialsAccount = func(_ context.Context, _ configstore.File, id, _ string) (aws.Config, error) {
				loads++
				if id != "123456789012" {
					t.Errorf("credential account = %s", id)
				}
				return aws.Config{Region: "payer-config"}, nil
			}
			failure := errors.New("AccessDenied")
			discoverExtendedSupportBillingCandidates = func(_ context.Context, cfg aws.Config, period cost.DateRange) (extendedsupport.BillingCandidates, error) {
				queries++
				if cfg.Region != "payer-config" || period.Start.Format("2006-01-02") != "2026-09-20" || period.End.Format("2006-01-02") != "2026-10-20" {
					t.Errorf("cfg=%+v period=%+v", cfg, period)
				}
				if tc.failed {
					return extendedsupport.BillingCandidates{}, failure
				}
				ids := []string{"222222222222", "123456789012", "999999999999"}
				if tc.empty {
					ids = nil
				}
				return extendedsupport.BillingCandidates{Period: period, UsageTypes: []string{"RDS:ExtendedSupport"}, AccountIDs: ids}, nil
			}
			cmd := &cobra.Command{}
			cmd.SetContext(context.Background())
			days := 30
			if tc.invalidDays {
				days = 0
			}
			selected, metadata, err := filterExtendedSupportBillingTargets(cmd, cfg, tc.sel, targets, tc.fullScan, days, time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC), nil)
			if tc.failed {
				if !errors.Is(err, failure) || !strings.Contains(err.Error(), "--full-scan") || !strings.Contains(err.Error(), "payer (123456789012)") || selected != nil || metadata != nil {
					t.Fatalf("selected=%v metadata=%+v error=%v", selected, metadata, err)
				}
				return
			}
			if tc.invalidDays {
				if err == nil || loads != 0 || queries != 0 {
					t.Fatalf("loads=%d queries=%d error=%v", loads, queries, err)
				}
				return
			}
			if err != nil || len(selected) != tc.want {
				t.Fatalf("selected=%v metadata=%+v error=%v", selected, metadata, err)
			}
			if tc.want == 2 {
				if metadata != nil || loads != 0 || queries != 0 {
					t.Fatalf("bypass made a billing query: metadata=%+v loads=%d queries=%d", metadata, loads, queries)
				}
			} else {
				if loads != 1 || queries != 1 || metadata == nil || metadata.EligibleAccountCount != 2 || metadata.SelectedAccountCount != tc.want || metadata.PayerAccountID != "123456789012" {
					t.Fatalf("metadata=%+v loads=%d queries=%d", metadata, loads, queries)
				}
				if !tc.empty && !reflect.DeepEqual(selected[0], targets[1]) {
					t.Fatal("target display metadata or role routing was lost")
				}
			}
		})
	}
}

func TestRunExtendedSupportListBillingSelectionControlsScanning(t *testing.T) {
	for _, scenario := range []string{"selected account recovers", "no billed accounts", "full scan", "billing failure"} {
		t.Run(scenario, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.yaml")
			if err := configstore.RegisterAWSAccount(path, "123456789012", "payer"); err != nil {
				t.Fatal(err)
			}
			prevFlags := awsFlags
			prevPayer, prevAliases, prevAccount, prevOU, prevTag := extendedSupportListPayer, extendedSupportListAccountAliases, extendedSupportListAccount, extendedSupportListOU, extendedSupportListTag
			prevFormat, prevServices, prevQuiet := extendedSupportListFormat, extendedSupportListServices, extendedSupportListQuiet
			prevFull, prevDays := extendedSupportListFullScan, extendedSupportListBillingDays
			prevLoad, prevDiscover := loadAWSConfigForCredentialsAccount, discoverExtendedSupportBillingCandidates
			prevList, prevRoot, prevCostEnsure := listOrganizationMemberAccounts, organizationRootID, ensureCostCredentials
			prevEnsure, prevPrepare, prevFetch, prevRetry := ensureExtendedSupportCredentials, prepareExtendedSupportTargets, extendedSupportListFetch, extendedSupportListRetry
			t.Cleanup(func() {
				awsFlags = prevFlags
				extendedSupportListPayer, extendedSupportListAccountAliases, extendedSupportListAccount, extendedSupportListOU, extendedSupportListTag = prevPayer, prevAliases, prevAccount, prevOU, prevTag
				extendedSupportListFormat, extendedSupportListServices, extendedSupportListQuiet = prevFormat, prevServices, prevQuiet
				extendedSupportListFullScan, extendedSupportListBillingDays = prevFull, prevDays
				loadAWSConfigForCredentialsAccount, discoverExtendedSupportBillingCandidates = prevLoad, prevDiscover
				listOrganizationMemberAccounts, organizationRootID, ensureCostCredentials = prevList, prevRoot, prevCostEnsure
				ensureExtendedSupportCredentials, prepareExtendedSupportTargets, extendedSupportListFetch, extendedSupportListRetry = prevEnsure, prevPrepare, prevFetch, prevRetry
			})
			awsFlags.ConfigPath = path
			extendedSupportListPayer, extendedSupportListAccountAliases, extendedSupportListAccount, extendedSupportListOU, extendedSupportListTag = "payer", "", "", "", ""
			extendedSupportListFormat, extendedSupportListServices, extendedSupportListQuiet = string(output.FormatJSON), "rds", true
			extendedSupportListFullScan, extendedSupportListBillingDays = scenario == "full scan", 30
			loadAWSConfigForCredentialsAccount = func(context.Context, configstore.File, string, string) (aws.Config, error) { return aws.Config{}, nil }
			ensureCostCredentials = func(context.Context, *cobra.Command, configstore.File, []cost.AccountTarget, string, string, string) error {
				return nil
			}
			organizationRootID = func(context.Context, aws.Config) (string, error) { return "r-root", nil }
			listOrganizationMemberAccounts = func(context.Context, aws.Config, string, coreaccount.ListAccountsInOUOptions) ([]coreaccount.OrganizationAccount, error) {
				return []coreaccount.OrganizationAccount{{ID: "111111111111", Name: "One"}, {ID: "222222222222", Name: "Two"}, {ID: "333333333333", Name: "Three"}}, nil
			}
			queries, prepared, fetched, retried, ensured := 0, 0, 0, 0, 0
			discoverExtendedSupportBillingCandidates = func(_ context.Context, _ aws.Config, period cost.DateRange) (extendedsupport.BillingCandidates, error) {
				queries++
				if scenario == "billing failure" {
					return extendedsupport.BillingCandidates{}, errors.New("AccessDenied")
				}
				ids := []string{"222222222222"}
				if scenario == "no billed accounts" {
					ids = nil
				}
				return extendedsupport.BillingCandidates{Period: period, UsageTypes: []string{"RDS:ExtendedSupport"}, AccountIDs: ids}, nil
			}
			ensureExtendedSupportCredentials = func(_ *cobra.Command, _ configstore.File, targets []cost.AccountTarget, _, _, _ string) error {
				ensured++
				if scenario != "full scan" && (len(targets) != 1 || targets[0].AccountID != "222222222222") {
					t.Error("credentials prepared for an excluded account")
				}
				return nil
			}
			prepareExtendedSupportTargets = func(_ *cobra.Command, _ configstore.File, targets []cost.AccountTarget, _, _, _ string, _ int, _ *progress.Bar) ([]extendedsupport.AccountTarget, []extendedsupport.AccountWarning, error) {
				prepared++
				var out []extendedsupport.AccountTarget
				for _, target := range targets {
					if scenario != "full scan" && target.AccountID != "222222222222" {
						t.Error("excluded account prepared")
					}
					out = append(out, extendedsupport.AccountTarget{AccountID: target.AccountID})
				}
				return out, nil, nil
			}
			extendedSupportListFetch = func(_ context.Context, q extendedsupport.Query) (extendedsupport.Result, error) {
				fetched++
				if scenario == "full scan" {
					if len(q.Targets) != 3 {
						t.Error("full scan lost targets")
					}
					return extendedsupport.Result{}, nil
				}
				if len(q.Targets) != 1 || q.Targets[0].AccountID != "222222222222" {
					t.Error("excluded account scanned")
				}
				return extendedsupport.Result{Summary: extendedsupport.Summary{SkippedAccounts: []extendedsupport.AccountWarning{{AccountID: "222222222222", Message: "temporary failure"}}}}, nil
			}
			extendedSupportListRetry = func(_ context.Context, q extendedsupport.Query, _ extendedsupport.Result) (extendedsupport.Result, error) {
				retried++
				if len(q.Targets) != 1 || q.Targets[0].AccountID != "222222222222" {
					t.Error("retry escaped billing selection")
				}
				return extendedsupport.Result{Records: []extendedsupport.Record{{AccountID: "222222222222", Service: extendedsupport.ServiceRDS, ResourceID: "database"}}, Summary: extendedsupport.Summary{TotalCount: 1}}, nil
			}
			cmd := &cobra.Command{}
			cmd.SetContext(context.Background())
			out := new(bytes.Buffer)
			cmd.SetOut(out)
			cmd.SetErr(new(bytes.Buffer))
			err := runExtendedSupportList(cmd, nil)
			if scenario == "billing failure" {
				if err == nil || out.Len() != 0 || prepared+fetched+retried+ensured != 0 {
					t.Fatalf("error=%v output=%s calls=%d/%d/%d/%d", err, out, prepared, fetched, retried, ensured)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			var result extendedsupport.Result
			if err := json.Unmarshal(out.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			switch scenario {
			case "selected account recovers":
				if queries != 1 || prepared != 2 || fetched != 1 || retried != 1 || result.Summary.BillingSelection == nil || result.Summary.BillingSelection.SelectedAccountCount != 1 || result.Summary.BillingSelection.EligibleAccountCount != 3 || result.Summary.TotalCount != 1 || len(result.Summary.SkippedAccounts) != 0 {
					t.Fatalf("result=%+v calls=%d/%d/%d/%d", result, queries, prepared, fetched, retried)
				}
			case "no billed accounts":
				if queries != 1 || prepared+fetched+retried+ensured != 0 || result.Summary.BillingSelection == nil || result.Summary.BillingSelection.SelectedAccountCount != 0 || len(result.Summary.SkippedAccounts) != 0 {
					t.Fatalf("result=%+v calls=%d/%d/%d/%d", result, queries, prepared, fetched, retried)
				}
			case "full scan":
				if queries != 0 || prepared != 1 || fetched != 1 || result.Summary.BillingSelection != nil {
					t.Fatalf("result=%+v calls=%d/%d/%d", result, queries, prepared, fetched)
				}
			}
		})
	}
}
