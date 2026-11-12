package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/openshift-online/finops-tools/cli/internal/configstore"
	coreaccount "github.com/openshift-online/finops-tools/core/account"
	"github.com/openshift-online/finops-tools/core/cost"
	"github.com/openshift-online/finops-tools/core/extendedsupport"
	"github.com/spf13/cobra"
)

func TestExtendedSupportCostDefaultsAndPeriods(t *testing.T) {
	previousNow := extendedSupportCostNow
	extendedSupportCostNow = func() time.Time { return time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC) }
	t.Cleanup(func() { extendedSupportCostNow = previousNow })
	for _, tc := range []struct {
		name                    string
		args                    []string
		start, end, granularity string
		group                   extendedsupport.CostGroupBy
		defaultPeriodNotice     bool
	}{
		{name: "defaults", start: "2026-08-04", end: "2026-11-02", granularity: "total", group: extendedsupport.CostGroupByService, defaultPeriodNotice: true},
		{name: "default account totals", args: []string{"--group-by", "account"}, start: "2026-08-04", end: "2026-11-02", granularity: "total", group: extendedsupport.CostGroupByAccount, defaultPeriodNotice: true},
		{name: "default service account totals", args: []string{"--group-by", "service,account"}, start: "2026-08-04", end: "2026-11-02", granularity: "total", group: extendedsupport.CostGroupByServiceAccount, defaultPeriodNotice: true},
		{name: "explicit monthly granularity", args: []string{"--granularity", "monthly"}, start: "2026-08-04", end: "2026-11-02", granularity: "monthly"},
		{name: "explicit default flags", args: []string{"--days", "90", "--exclude-recent-days", "2"}, start: "2026-08-04", end: "2026-11-02", granularity: "total"},
		{name: "disable exclusion", args: []string{"--exclude-recent-days", "0"}, start: "2026-08-06", end: "2026-11-04", granularity: "total", defaultPeriodNotice: true},
		{name: "custom exclusion", args: []string{"--days", "7", "--exclude-recent-days", "1"}, start: "2026-10-27", end: "2026-11-03", granularity: "total"},
		{name: "default window daily override", args: []string{"--granularity", "daily"}, start: "2026-08-04", end: "2026-11-02", granularity: "daily"},
		{name: "exactly 90 without exclusion", args: []string{"--from", "2026-08-06", "--to", "2026-11-03", "--exclude-recent-days", "0"}, start: "2026-08-06", end: "2026-11-04", granularity: "total"},
		{name: "91 days without exclusion", args: []string{"--from", "2026-08-05", "--to", "2026-11-03", "--exclude-recent-days", "0"}, start: "2026-08-05", end: "2026-11-04", granularity: "total"},
		{name: "explicit range at buffered cutoff", args: []string{"--from", "2026-10-01", "--to", "2026-11-01"}, start: "2026-10-01", end: "2026-11-02", granularity: "total"},
		{name: "explicit range account totals", args: []string{"--from", "2026-08-01", "--to", "2026-10-31", "--group-by", "account"}, start: "2026-08-01", end: "2026-11-01", granularity: "total", group: extendedsupport.CostGroupByAccount},
		{name: "explicit range service account totals", args: []string{"--from", "2026-08-01", "--to", "2026-10-31", "--group-by", "service,account"}, start: "2026-08-01", end: "2026-11-01", granularity: "total", group: extendedsupport.CostGroupByServiceAccount},
		{name: "explicit range monthly override", args: []string{"--from", "2026-08-01", "--to", "2026-10-31", "--granularity", "monthly"}, start: "2026-08-01", end: "2026-11-01", granularity: "monthly"},
		{name: "explicit range daily override", args: []string{"--from", "2026-08-01", "--to", "2026-10-31", "--granularity", "daily"}, start: "2026-08-01", end: "2026-11-01", granularity: "daily"},
		{name: "leap year 90", args: []string{"--from", "2024-02-01", "--to", "2024-04-30"}, start: "2024-02-01", end: "2024-05-01", granularity: "total"},
		{name: "from only", args: []string{"--from", "2026-11-01"}, start: "2026-11-01", end: "2026-11-02", granularity: "total"},
		{name: "from only no exclusion", args: []string{"--from", "2026-11-01", "--exclude-recent-days", "0"}, start: "2026-11-01", end: "2026-11-04", granularity: "total"},
		{name: "short day override defaults to totals", args: []string{"--days", "7"}, start: "2026-10-26", end: "2026-11-02", granularity: "total"},
		{name: "calendar months", args: []string{"--months", "3"}, start: "2026-08-01", end: "2026-11-02", granularity: "total"},
		{name: "one day monthly override", args: []string{"--from", "2026-10-01", "--to", "2026-10-01", "--granularity", "monthly"}, start: "2026-10-01", end: "2026-10-02", granularity: "monthly"},
		{name: "long daily override", args: []string{"--days", "91", "--granularity", "daily", "--group-by", "service,account"}, start: "2026-08-03", end: "2026-11-02", granularity: "daily", group: extendedsupport.CostGroupByServiceAccount},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cmd := newExtendedSupportGetCostCommand()
			args := append([]string{"--payer", "payer"}, tc.args...)
			if err := cmd.ParseFlags(args); err != nil {
				t.Fatal(err)
			}
			if err := cmd.PreRunE(cmd, nil); err != nil {
				t.Fatal(err)
			}
			// Inspect the query passed to the core by stopping before credential work.
			previousResolve := resolveExtendedSupportCostTargets
			previousFetch := fetchExtendedSupportCosts
			previousConfig := awsFlags.ConfigPath
			t.Cleanup(func() {
				resolveExtendedSupportCostTargets = previousResolve
				fetchExtendedSupportCosts = previousFetch
				awsFlags.ConfigPath = previousConfig
			})
			awsFlags.ConfigPath = filepath.Join(t.TempDir(), "missing.yaml")
			resolveExtendedSupportCostTargets = func(_ *cobra.Command, _ configstore.File, _ *costTargetSelector, _ costStepper) ([]cost.AccountTarget, error) {
				return nil, nil
			}
			buf := new(bytes.Buffer)
			cmd.SetOut(buf)
			cmd.SetErr(new(bytes.Buffer))
			if err := cmd.Flags().Set("format", "json"); err != nil {
				t.Fatal(err)
			}
			if err := cmd.RunE(cmd, nil); err != nil {
				t.Fatal(err)
			}
			var result extendedsupport.CostResult
			if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			wantGroup := tc.group
			if wantGroup == "" {
				wantGroup = extendedsupport.CostGroupByService
			}
			end, _ := cost.ParseDate(tc.end)
			if result.StartDate != tc.start || result.EndDate != cost.FormatDate(end.AddDate(0, 0, -1)) || result.Granularity != tc.granularity || result.GroupBy != wantGroup || len(result.Services) != 4 || result.Metric != "NetUnblendedCost" {
				t.Fatalf("result=%+v", result)
			}
			if strings.Contains(strings.Join(result.Notices, " "), "Defaulting to totals") != tc.defaultPeriodNotice {
				t.Fatalf("default-period notice=%v", result.Notices)
			}
		})
	}
}

func TestExtendedSupportCostRejectsInvalidInputsBeforeAWS(t *testing.T) {
	for _, args := range [][]string{
		{"--services", "ec2"}, {"--services", ","}, {"--group-by", "rds"}, {"--group-by", "ou"},
		{"--granularity", "hourly"}, {"--metric", "UsageQuantity"}, {"--days", "0"},
		{"--to", "2026-01-01"}, {"--from", "2026-10-02", "--to", "2026-10-01"},
		{"--from", "2026-02-30"}, {"--from", "2099-01-01"},
		{"--from", "2026-01-01", "--days", "90"}, {"--skip-org-cache", "--refresh-org-cache"},
		{"--regions", ","}, {"--skip-regions", ""}, {"--skip-regions", " , "},
		{"--months", "0"}, {"--exclude-recent-days", "-1"}, {"--days", "7", "--months", "1"},
		{"--from", "2026-01-01", "--months", "1"}, {"--from", "2026-01-01", "--to", "2099-01-01"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			cmd := newExtendedSupportGetCostCommand()
			if err := cmd.ParseFlags(append([]string{"--payer", "payer"}, args...)); err != nil {
				t.Fatal(err)
			}
			if err := cmd.PreRunE(cmd, nil); err == nil {
				t.Fatal("invalid arguments accepted")
			}
		})
	}
	cmd := newExtendedSupportGetCostCommand()
	if err := cmd.PreRunE(cmd, nil); err == nil {
		t.Fatal("missing target accepted")
	}
}

func TestExtendedSupportCostUsesSharedPeriodRulesWithoutChangingOtherCommands(t *testing.T) {
	before := periodFlags
	cmd := newExtendedSupportGetCostCommand()
	if err := cmd.ParseFlags([]string{"--payer", "payer", "--days", "7", "--exclude-recent-days", "0"}); err != nil {
		t.Fatal(err)
	}
	if periodFlags != before {
		t.Fatal("extended-support period flags changed other commands' state")
	}
	for _, other := range []*cobra.Command{accountGetCostCmd, reportCreateCmd} {
		if other.Flags().Lookup("days").DefValue != "0" || other.Flags().Lookup("exclude-recent-days").DefValue != "0" {
			t.Fatalf("changed defaults on %s", other.Name())
		}
	}
	previousNow := extendedSupportCostNow
	extendedSupportCostNow = func() time.Time { return time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC) }
	t.Cleanup(func() { extendedSupportCostNow = previousNow })
	cmd = newExtendedSupportGetCostCommand()
	if err := cmd.ParseFlags([]string{"--payer", "payer", "--from", "2026-10-01", "--to", "2026-11-02"}); err != nil {
		t.Fatal(err)
	}
	if err := cmd.PreRunE(cmd, nil); err == nil || !strings.Contains(err.Error(), "end 2026-11-02 is after 2026-11-01") {
		t.Fatalf("shared cutoff validation not applied: %v", err)
	}
}

func TestExtendedSupportCostPayerSelectionDoesNotListMembers(t *testing.T) {
	cfg := configstore.Default()
	var err error
	if cfg, err = cfg.SetAWSAlias("payer-a", "123456789012"); err != nil {
		t.Fatal(err)
	}
	if cfg, err = cfg.SetAWSAlias("payer-b", "987654321098"); err != nil {
		t.Fatal(err)
	}
	previous := listOrganizationMemberAccounts
	listOrganizationMemberAccounts = func(context.Context, aws.Config, string, coreaccount.ListAccountsInOUOptions) ([]coreaccount.OrganizationAccount, error) {
		t.Fatal("historical report enumerated today's active accounts")
		return nil, nil
	}
	t.Cleanup(func() { listOrganizationMemberAccounts = previous })
	sel := costTargetSelector{PayerAliases: []string{"payer-a", "payer-b", "payer-a"}}
	targets, err := resolveExtendedSupportCostTargetsImpl(&cobra.Command{}, cfg, &sel, nil)
	if err != nil || len(targets) != 2 || targets[0].ScopeToAccount() || targets[1].ScopeToAccount() {
		t.Fatalf("targets=%+v err=%v", targets, err)
	}
	sel = costTargetSelector{Aliases: []string{"payer-a"}}
	targets, err = resolveExtendedSupportCostTargetsImpl(&cobra.Command{}, cfg, &sel, nil)
	if err != nil || len(targets) != 1 || !targets[0].ScopeToAccount() {
		t.Fatalf("explicit payer leaked organization costs: %+v err=%v", targets, err)
	}
}

func TestExtendedSupportCostCommandUsesOnlyBillingCredentialsAndWritesOutput(t *testing.T) {
	previousResolve, previousFetch := resolveExtendedSupportCostTargets, fetchExtendedSupportCosts
	previousEnsure, previousLoad := ensureExtendedSupportCostCredentials, loadAWSConfigForCredentialsAccount
	previousConfig := awsFlags.ConfigPath
	t.Cleanup(func() {
		resolveExtendedSupportCostTargets, fetchExtendedSupportCosts = previousResolve, previousFetch
		ensureExtendedSupportCostCredentials, loadAWSConfigForCredentialsAccount = previousEnsure, previousLoad
		awsFlags.ConfigPath = previousConfig
	})
	awsFlags.ConfigPath = filepath.Join(t.TempDir(), "config.yaml")
	loads, ensures := 0, 0
	resolveExtendedSupportCostTargets = func(_ *cobra.Command, _ configstore.File, sel *costTargetSelector, _ costStepper) ([]cost.AccountTarget, error) {
		if !sel.IncludeClosedAccounts {
			t.Fatal("historical selection excluded closed accounts")
		}
		return []cost.AccountTarget{{AccountID: "111111111111", PayerAccountID: "123456789012"}, {AccountID: "222222222222", PayerAccountID: "123456789012"}}, nil
	}
	ensureExtendedSupportCostCredentials = func(_ context.Context, _ *cobra.Command, _ configstore.File, _ []cost.AccountTarget, _, _, _ string) error {
		ensures++
		return nil
	}
	loadAWSConfigForCredentialsAccount = func(_ context.Context, _ configstore.File, id, _ string) (aws.Config, error) {
		loads++
		if id != "123456789012" {
			t.Fatalf("loaded member credentials: %s", id)
		}
		return aws.Config{Region: "billing"}, nil
	}
	fetchExtendedSupportCosts = func(_ context.Context, q extendedsupport.CostQuery) (extendedsupport.CostResult, error) {
		if !reflect.DeepEqual(q.Services, []extendedsupport.Service{extendedsupport.ServiceRDS, extendedsupport.ServiceEKS}) || q.GroupBy != extendedsupport.CostGroupByAccount || q.Granularity != "monthly" || !q.TotalsOnly || len(q.Targets) != 2 || q.Targets[1].AWSConfig.Region != "billing" {
			t.Fatalf("query=%+v", q)
		}
		if !reflect.DeepEqual(q.Regions, []string{"us-east-1", "us-west-2"}) || !reflect.DeepEqual(q.SkipRegions, []string{"eu-west-1", "us-west-2"}) {
			t.Fatalf("region scope=%+v", q)
		}
		result := extendedsupport.NewCostResult(q)
		result.Amount = 10
		return result, nil
	}
	cmd := newExtendedSupportGetCostCommand()
	outputPath := filepath.Join(t.TempDir(), "costs.json")
	if err := cmd.ParseFlags([]string{"--payer", "payer", "--days", "91", "--services", "rds,eks", "--group-by", "account", "--format", "json", "--output", outputPath,
		"--regions", " us-west-2,us-east-1,us-east-1 ", "--skip-regions", "us-west-2", "--skip-regions", "eu-west-1,us-west-2"}); err != nil {
		t.Fatal(err)
	}
	cmd.SetOut(new(bytes.Buffer))
	cmd.SetErr(new(bytes.Buffer))
	if err := cmd.RunE(cmd, nil); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(outputPath)
	if err != nil || !strings.Contains(string(data), `"amount": 10`) || loads != 1 || ensures != 1 {
		t.Fatalf("output=%s loads=%d ensures=%d err=%v", data, loads, ensures, err)
	}
	// A billing failure must not replace a previously saved successful report.
	fetchExtendedSupportCosts = func(context.Context, extendedsupport.CostQuery) (extendedsupport.CostResult, error) {
		return extendedsupport.CostResult{}, errors.New("billing denied")
	}
	if err := cmd.RunE(cmd, nil); err == nil {
		t.Fatal("billing failure hidden")
	}
	after, err := os.ReadFile(outputPath)
	if err != nil || !bytes.Equal(data, after) {
		t.Fatal("failed query replaced the existing output file")
	}
	// An explicitly empty region scope also bypasses billing authentication.
	if err := cmd.Flags().Set("regions", "us-west-2"); err != nil {
		t.Fatal(err)
	}
	if err := cmd.RunE(cmd, nil); err != nil {
		t.Fatal(err)
	}
	if loads != 2 || ensures != 2 {
		t.Fatalf("empty region scope authenticated: loads=%d ensures=%d", loads, ensures)
	}
	emptyData, err := os.ReadFile(outputPath)
	if err != nil || !strings.Contains(string(emptyData), "All requested billing regions are excluded") {
		t.Fatalf("empty result=%s err=%v", emptyData, err)
	}
}
