package cmd

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/openshift-online/finops-tools/cli/internal/configstore"
	"github.com/openshift-online/finops-tools/cli/internal/output"
	"github.com/openshift-online/finops-tools/cli/internal/progress"
	"github.com/openshift-online/finops-tools/core/cost"
	"github.com/openshift-online/finops-tools/core/snapshot"
	"github.com/spf13/cobra"
)

func TestSnapshotListPreRunRejectsInvalidOlderThanDays(t *testing.T) {
	snapshotListOlderThanDays = 0
	snapshotListTypes = "ebs,rds"
	snapshotListRegions = ""
	snapshotListAccount = "123456789012"
	snapshotListAccountAliases = ""
	snapshotListFormat = string(output.FormatPrettyPrint)
	t.Cleanup(func() {
		snapshotListOlderThanDays = snapshot.DefaultOlderThanDays
		snapshotListTypes = "ebs,rds"
		snapshotListRegions = ""
		snapshotListAccount = ""
		snapshotListAccountAliases = ""
	})

	cmd := snapshotListCmd
	cmd.SetOut(new(bytes.Buffer))
	cmd.SetErr(new(bytes.Buffer))
	if err := cmd.PreRunE(cmd, nil); err == nil {
		t.Fatal("expected error for older-than-days <= 0")
	}
}

func TestSnapshotListPreRunRejectsUnknownType(t *testing.T) {
	snapshotListOlderThanDays = 180
	snapshotListTypes = "unknown"
	snapshotListAccount = "123456789012"
	snapshotListAccountAliases = ""
	snapshotListFormat = string(output.FormatPrettyPrint)
	t.Cleanup(func() {
		snapshotListOlderThanDays = snapshot.DefaultOlderThanDays
		snapshotListTypes = "ebs,rds"
		snapshotListAccount = ""
		snapshotListFormat = string(output.FormatPrettyPrint)
	})

	cmd := snapshotListCmd
	cmd.SetOut(new(bytes.Buffer))
	cmd.SetErr(new(bytes.Buffer))
	if err := cmd.PreRunE(cmd, nil); err == nil {
		t.Fatal("expected error for unknown type")
	}
}

func TestRunSnapshotListUsesFetchHook(t *testing.T) {
	prevSkip := snapshotListSkipRegions
	snapshotListSkipRegions = []string{" us-west-2 ", "us-west-2"}
	t.Cleanup(func() { snapshotListSkipRegions = prevSkip })
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := configstore.RegisterAWSAccount(path, "123456789012", "rh-control"); err != nil {
		t.Fatal(err)
	}

	prevFetch := snapshotListFetch
	prevPrepare := prepareSnapshotTargets
	prevEnsure := ensureSnapshotCredentials
	prevBilled := fetchSnapshotBilledCosts
	prevRegionalBilled := fetchRegionalSnapshotBilledCosts
	t.Cleanup(func() {
		snapshotListFetch = prevFetch
		prepareSnapshotTargets = prevPrepare
		ensureSnapshotCredentials = prevEnsure
		fetchSnapshotBilledCosts = prevBilled
		fetchRegionalSnapshotBilledCosts = prevRegionalBilled
		awsFlags.ConfigPath = ""
	})

	snapshotListFetch = func(_ context.Context, q snapshot.Query) (snapshot.Result, error) {
		if len(q.Targets) != 1 || q.Targets[0].AccountID != "123456789012" {
			t.Fatalf("targets = %#v", q.Targets)
		}
		if q.OlderThan != 90*24*time.Hour {
			t.Fatalf("older than = %v", q.OlderThan)
		}
		if !reflect.DeepEqual(q.SkipRegions, []string{"us-west-2"}) {
			t.Fatalf("skip regions = %v", q.SkipRegions)
		}
		return snapshot.Result{
			Summary: snapshot.Summary{
				TotalCount:              1,
				ScanScopes:              []snapshot.AccountScanScope{{AccountID: "123456789012", Regions: []string{"us-east-1"}}},
				EstimatedMonthlyCostUSD: 5,
				OlderThanDays:           90,
				CostDisclaimer:          "Estimates use volume or allocated size; actual EBS snapshot billing may be lower.",
			},
		}, nil
	}
	ensureSnapshotCredentials = func(_ *cobra.Command, _ configstore.File, _ []cost.AccountTarget, _, _, _ string) error {
		return nil
	}
	var billingTime time.Time
	fetchSnapshotBilledCosts = func(_ context.Context, _ configstore.File, _ []cost.AccountTarget, _ string, now time.Time, _ int) ([]snapshot.AccountBilledSnapshotCosts, error) {
		billingTime = now
		return []snapshot.AccountBilledSnapshotCosts{{AccountID: "123456789012", EBSSnapshotUSD: 1000}}, nil
	}
	var regionalBillingErr error
	fetchRegionalSnapshotBilledCosts = func(_ context.Context, _ configstore.File, _ []cost.AccountTarget, _ string, scopes []snapshot.AccountScanScope, now time.Time, _ int) ([]snapshot.AccountBilledSnapshotCosts, error) {
		if len(scopes) != 1 || !reflect.DeepEqual(scopes[0].Regions, []string{"us-east-1"}) {
			t.Fatalf("billing scopes = %+v", scopes)
		}
		if !now.Equal(billingTime) {
			t.Fatal("billing periods must share the same time anchor")
		}
		if regionalBillingErr != nil {
			return nil, regionalBillingErr
		}
		return []snapshot.AccountBilledSnapshotCosts{{AccountID: "123456789012", EBSSnapshotUSD: 100}}, nil
	}
	prepareSnapshotTargets = func(_ *cobra.Command, _ configstore.File, targets []cost.AccountTarget, _, _, _ string, _ int, _ *progress.Bar) ([]snapshot.AccountTarget, []snapshot.AccountWarning, error) {
		out := make([]snapshot.AccountTarget, 0, len(targets))
		for _, target := range targets {
			out = append(out, snapshot.AccountTarget{AccountID: target.AccountID})
		}
		return out, nil, nil
	}

	awsFlags.ConfigPath = path
	snapshotListAccount = ""
	snapshotListAccountAliases = "rh-control"
	snapshotListOlderThanDays = 90
	snapshotListTypes = "ebs"
	snapshotListFormat = string(output.FormatJSON)
	snapshotListQuiet = true
	t.Cleanup(func() {
		snapshotListAccount = ""
		snapshotListAccountAliases = ""
		snapshotListOlderThanDays = snapshot.DefaultOlderThanDays
		snapshotListTypes = "ebs,rds"
		snapshotListFormat = string(output.FormatPrettyPrint)
		snapshotListQuiet = false
	})

	buf := new(bytes.Buffer)
	cmd := snapshotListCmd
	cmd.SetOut(buf)
	cmd.SetErr(new(bytes.Buffer))
	if err := runSnapshotList(cmd, nil); err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(buf.Bytes(), []byte(`"total_count": 1`)) {
		t.Fatalf("output = %s", buf.String())
	}
	for _, want := range []string{`"billed_costs":`, `"regional_billed_costs":`, `"ebs_snapshot_usd": 1000`, `"ebs_snapshot_usd": 100`} {
		if !bytes.Contains(buf.Bytes(), []byte(want)) {
			t.Fatalf("output missing %s: %s", want, buf)
		}
	}

	// Regional billing failure preserves account-wide comparison and API estimates.
	regionalBillingErr = errors.New("regional query failed")
	buf.Reset()
	if err := runSnapshotList(cmd, nil); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(buf.Bytes(), []byte(`"regional_billed_costs":`)) || !bytes.Contains(buf.Bytes(), []byte(`"billed_costs":`)) || !bytes.Contains(buf.Bytes(), []byte(`"estimated_monthly_cost_usd": 5`)) {
		t.Fatalf("regional billing failure must preserve estimates and account-wide totals: %s", buf)
	}

	// Preparation failures must still allow the payer's account-wide billing query.
	prepareSnapshotTargets = func(_ *cobra.Command, _ configstore.File, _ []cost.AccountTarget, _, _, _ string, _ int, _ *progress.Bar) ([]snapshot.AccountTarget, []snapshot.AccountWarning, error) {
		return nil, []snapshot.AccountWarning{{AccountID: "123456789012", Message: "assume role failed"}}, nil
	}
	snapshotListFetch = func(context.Context, snapshot.Query) (snapshot.Result, error) {
		t.Fatal("no scan should run without prepared targets")
		return snapshot.Result{}, nil
	}
	fetchRegionalSnapshotBilledCosts = func(_ context.Context, _ configstore.File, _ []cost.AccountTarget, _ string, scopes []snapshot.AccountScanScope, _ time.Time, _ int) ([]snapshot.AccountBilledSnapshotCosts, error) {
		if len(scopes) != 0 {
			t.Fatalf("unexpected scanned regions: %v", scopes)
		}
		return []snapshot.AccountBilledSnapshotCosts{{AccountID: "123456789012"}}, nil
	}
	buf.Reset()
	if err := runSnapshotList(cmd, nil); err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(buf.Bytes(), []byte(`"ebs_snapshot_usd": 1000`)) || !bytes.Contains(buf.Bytes(), []byte(`"skipped_accounts":`)) || !bytes.Contains(buf.Bytes(), []byte(`"regional_billed_costs":`)) {
		t.Fatalf("missing billing comparison for skipped account: %s", buf)
	}
}
