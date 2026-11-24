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
	"github.com/openshift-online/finops-tools/core/awshealth"
	"github.com/openshift-online/finops-tools/core/cost"
	"github.com/openshift-online/finops-tools/core/extendedsupport"
	"github.com/spf13/cobra"
)

func testUpcomingCommand(t *testing.T, args ...string) *cobra.Command {
	t.Helper()
	cmd := newExtendedSupportUpcomingCommand()
	cmd.SetOut(new(bytes.Buffer))
	cmd.SetErr(new(bytes.Buffer))
	if err := cmd.ParseFlags(args); err != nil {
		t.Fatal(err)
	}
	return cmd
}

func TestUpcomingRequiresPayerAndRejectsInventoryFlags(t *testing.T) {
	for _, args := range [][]string{
		{}, {"--account-id", "111111111111"}, {"--payer", "payer", "--account-id", "111111111111"},
		{"--payer", "payer", "--ou", "ou-example"}, {"--payer", "payer", "--tag", "owner=team"},
		{"--payer", "payer", "--full-scan"}, {"--payer", "payer", "--billing-lookback-days", "7"},
		{"--payer", "payer", "--role", "MemberRole"}, {"--payer", "payer", "--skip-org-cache"},
		{"--payer", "payer", "--refresh-org-cache"}, {"--payer", "payer", "--services", ","},
		{"--payer", "payer", "--service", ""}, {"--payer", "payer", "--service", ","},
		{"--payer", "payer", "--service", "rds", "--services", "eks"},
		{"--payer", "payer", "--format", "bad"}, {"--payer", "payer", "--workers", "-1"},
		{"--payer", "payer", "--regions", ","}, {"--payer", "payer", "--skip-regions", ""},
		{"--payer", "payer", "--health-request-interval", "0s"}, {"--payer", "payer", "--health-request-interval", "-1s"},
		{"--payer", "payer", "--lookahead-days", "0"}, {"--payer", "payer", "--lookahead-days", "-1"},
		{"--payer", "payer", "--lookahead-days", "1.5"}, {"--payer", "payer", "--lookahead-days", "abc"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			cmd := newExtendedSupportUpcomingCommand()
			if err := cmd.ParseFlags(args); err != nil {
				return // Inventory flags are not registered on the upcoming command.
			}
			if err := cmd.PreRunE(cmd, nil); err == nil {
				t.Fatalf("invalid upcoming args accepted: %v", args)
			}
		})
	}
	cmd := testUpcomingCommand(t, "--payer", "payer-a,payer-b")
	if err := cmd.PreRunE(cmd, nil); err != nil {
		t.Fatal(err)
	}
	if services, err := cmd.Flags().GetString("services"); err != nil || services != "" {
		t.Fatal(services, err)
	}
	if interval, err := cmd.Flags().GetDuration("health-request-interval"); err != nil || interval != 100*time.Millisecond {
		t.Fatal(interval, err)
	}
}

func TestUpcomingOwnerDefaultsAndResolvedOverride(t *testing.T) {
	for _, tc := range []struct {
		name          string
		args          []string
		owner, hidden bool
	}{
		{name: "default"},
		{name: "hide resolved", args: []string{"--hide-resolved"}, hidden: true},
		{name: "owner", args: []string{"--owner"}, owner: true, hidden: true},
		{name: "owner keeps resolved", args: []string{"--owner", "--hide-resolved=false"}, owner: true},
		{name: "override before owner", args: []string{"--hide-resolved=false", "--owner"}, owner: true},
		{name: "owner explicitly hides resolved", args: []string{"--owner", "--hide-resolved"}, owner: true, hidden: true},
		{name: "owner disabled", args: []string{"--owner=false"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cmd := testUpcomingCommand(t, append([]string{"--payer", "payer"}, tc.args...)...)
			f := &extendedSupportUpcomingFlags{}
			// Use parsed flags to check the owner default and explicit overrides.
			f.owner, _ = cmd.Flags().GetBool("owner")
			f.hideResolved, _ = cmd.Flags().GetBool("hide-resolved")
			f.workers, _ = cmd.Flags().GetInt("workers")
			f.payer, f.format, f.requestInterval = "payer", "json", time.Second
			q, _, err := f.query(cmd)
			if err != nil || q.IncludeOwners != tc.owner || q.HideResolved != tc.hidden {
				t.Fatal(q, err)
			}
		})
	}
}

func TestUpcomingIsIndependentSiblingOfList(t *testing.T) {
	registered, _, err := extendedSupportCmd.Find([]string{"upcoming"})
	if err != nil || registered.Name() != "upcoming" || registered.Parent() != extendedSupportCmd {
		t.Fatal(registered, err)
	}
	for _, flag := range []string{"upcoming", "health-request-interval"} {
		if extendedSupportListCmd.Flags().Lookup(flag) != nil {
			t.Fatalf("list still exposes --%s", flag)
		}
	}
	previousPayer, previousServices, previousFormat := extendedSupportListPayer, extendedSupportListServices, extendedSupportListFormat
	first := testUpcomingCommand(t, "--payer", "first", "--services", "lambda", "--format", "json", "--health-request-interval", "2s", "--hide-resolved", "--owner", "--lookahead-days", "90")
	if err := first.PreRunE(first, nil); err != nil {
		t.Fatal(err)
	}
	second := testUpcomingCommand(t, "--payer", "second")
	if err := second.PreRunE(second, nil); err != nil {
		t.Fatal(err)
	}
	services, _ := second.Flags().GetString("services")
	format, _ := second.Flags().GetString("format")
	interval, _ := second.Flags().GetDuration("health-request-interval")
	hideResolved, _ := second.Flags().GetBool("hide-resolved")
	owner, _ := second.Flags().GetBool("owner")
	lookahead, _ := second.Flags().GetInt("lookahead-days")
	if services != "" || format != "pretty-print" || interval != 100*time.Millisecond || hideResolved || owner || lookahead != 0 {
		t.Fatal("upcoming command instances share flag values")
	}
	if extendedSupportListPayer != previousPayer || extendedSupportListServices != previousServices || extendedSupportListFormat != previousFormat {
		t.Fatal("upcoming flags changed inventory list state")
	}
}

func TestUpcomingCommandUsesOnlyPayerCredentialsAndPreservesWarningsAndOutput(t *testing.T) {
	cmd := testUpcomingCommand(t, "--payer", "payer,payer-alias", "--service", " rDs ", "--regions", "us-east-1,global", "--skip-regions", "global", "--format", "json", "--quiet", "--health-request-interval", "2s", "--owner", "--lookahead-days", "90")
	oldFlags := awsFlags
	oldFetch, oldEnsure, oldLoad := fetchExtendedSupportUpcoming, ensureExtendedSupportUpcomingCredentials, loadAWSConfigForCredentialsAccount
	oldScan, oldBilling := extendedSupportListFetch, discoverExtendedSupportBillingCandidates
	t.Cleanup(func() {
		awsFlags = oldFlags
		fetchExtendedSupportUpcoming, ensureExtendedSupportUpcomingCredentials, loadAWSConfigForCredentialsAccount = oldFetch, oldEnsure, oldLoad
		extendedSupportListFetch, discoverExtendedSupportBillingCandidates = oldScan, oldBilling
	})
	cfg := configstore.Default()
	var err error
	cfg, err = cfg.SetAWSAlias("payer", "123456789012")
	if err != nil {
		t.Fatal(err)
	}
	cfg, err = cfg.SetAWSAlias("payer-alias", "123456789012")
	if err != nil {
		t.Fatal(err)
	}
	awsFlags.ConfigPath = filepath.Join(t.TempDir(), "config.yaml")
	if err := configstore.Save(awsFlags.ConfigPath, cfg); err != nil {
		t.Fatal(err)
	}
	ensures, loads := 0, 0
	ensureExtendedSupportUpcomingCredentials = func(_ context.Context, _ *cobra.Command, _ configstore.File, targets []cost.AccountTarget, _, _, _ string) error {
		ensures++
		if len(targets) != 1 || targets[0].AccountID != "123456789012" {
			t.Fatal(targets)
		}
		return nil
	}
	loadAWSConfigForCredentialsAccount = func(_ context.Context, _ configstore.File, id, _ string) (aws.Config, error) {
		loads++
		if id != "123456789012" {
			t.Fatal("loaded member credentials", id)
		}
		return aws.Config{Region: "payer-config"}, nil
	}
	extendedSupportListFetch = func(context.Context, extendedsupport.Query) (extendedsupport.Result, error) {
		t.Fatal("upcoming performed resource scanning")
		return extendedsupport.Result{}, nil
	}
	discoverExtendedSupportBillingCandidates = func(context.Context, aws.Config, cost.DateRange) (extendedsupport.BillingCandidates, error) {
		t.Fatal("upcoming queried billing")
		return extendedsupport.BillingCandidates{}, nil
	}
	fetchExtendedSupportUpcoming = func(_ context.Context, q awshealth.Query) (awshealth.Result, error) {
		if len(q.Targets) != 1 || q.Targets[0].AWSConfig.Region != "payer-config" || !reflect.DeepEqual(q.Services, []string{"RDS"}) || len(q.Regions) != 2 || !reflect.DeepEqual(q.SkipRegions, []string{"global"}) || q.RequestInterval != 2*time.Second || !q.HideResolved || !q.IncludeOwners || q.LookaheadDays != 90 {
			t.Fatal(q)
		}
		return awshealth.Result{AsOf: q.Now, LookaheadDays: q.LookaheadDays, OwnersIncluded: q.IncludeOwners, Events: []awshealth.Event{{
			PayerAccountID: "123456789012", ARN: "event", Description: "Upcoming Extended Support",
			Accounts: []awshealth.AffectedAccount{{AccountID: "111111111111", OwnerEmail: "jdoe@redhat.com", Resources: []awshealth.Resource{{ResourceID: "db-production", Status: "PENDING"}}}},
		}}, Warnings: []awshealth.Warning{{PayerAccountID: "123456789012", Operation: "event_details", Message: "details denied"}}}, nil
	}
	path := filepath.Join(t.TempDir(), "upcoming.json")
	if err := cmd.Flags().Set("output", path); err != nil {
		t.Fatal(err)
	}
	if err := cmd.PreRunE(cmd, nil); err != nil {
		t.Fatal(err)
	}
	if err := cmd.RunE(cmd, nil); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	var decoded awshealth.Result
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &decoded); err != nil || decoded.Complete || len(decoded.Warnings) != 1 || ensures != 1 || loads != 1 || !decoded.OwnersIncluded || len(decoded.Events) != 1 || decoded.LookaheadDays != 90 {
		t.Fatal(string(data), ensures, loads, err)
	}
	if account := decoded.Events[0].Accounts[0]; account.OwnerEmail != "jdoe@redhat.com" || account.Resources[0].ResourceID != "db-production" || decoded.Events[0].Description != "Upcoming Extended Support" {
		t.Fatal(decoded)
	}
	if !strings.Contains(cmd.ErrOrStderr().(*bytes.Buffer).String(), "details denied") {
		t.Fatal("quiet hid a lookup warning")
	}
	fetchExtendedSupportUpcoming = func(context.Context, awshealth.Query) (awshealth.Result, error) {
		return awshealth.Result{}, errors.New("Health denied")
	}
	if err := cmd.RunE(cmd, nil); err == nil {
		t.Fatal("Health discovery failure hidden")
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(data, after) {
		t.Fatal("failed discovery replaced existing output")
	}
}

func TestUpcomingAllExcludedRegionsSkipsAuthentication(t *testing.T) {
	cmd := testUpcomingCommand(t, "--payer", "payer", "--regions", "us-east-1", "--skip-regions", "us-east-1", "--format", "json")
	oldFlags, oldEnsure := awsFlags, ensureExtendedSupportUpcomingCredentials
	t.Cleanup(func() { awsFlags = oldFlags; ensureExtendedSupportUpcomingCredentials = oldEnsure })
	cfg, err := configstore.Default().SetAWSAlias("payer", "123456789012")
	if err != nil {
		t.Fatal(err)
	}
	awsFlags.ConfigPath = filepath.Join(t.TempDir(), "config.yaml")
	if err := configstore.Save(awsFlags.ConfigPath, cfg); err != nil {
		t.Fatal(err)
	}
	ensureExtendedSupportUpcomingCredentials = func(context.Context, *cobra.Command, configstore.File, []cost.AccountTarget, string, string, string) error {
		t.Fatal("empty event scope authenticated")
		return nil
	}
	if err := cmd.RunE(cmd, nil); err != nil {
		t.Fatal(err)
	}
	var result awshealth.Result
	if err := json.Unmarshal(cmd.OutOrStdout().(*bytes.Buffer).Bytes(), &result); err != nil || !result.Complete || len(result.Events) != 0 {
		t.Fatal(result, err)
	}
}
