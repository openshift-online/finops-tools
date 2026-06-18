package cmd

import (
	"bytes"
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/openshift-online/finops-tools/cli/internal/configstore"
	"github.com/openshift-online/finops-tools/cli/internal/output"
	"github.com/openshift-online/finops-tools/core/snapshot"
	"github.com/openshift-online/finops-tools/core/cost"
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
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := configstore.RegisterAWSAccount(path, "123456789012", "rh-control"); err != nil {
		t.Fatal(err)
	}

	prevFetch := snapshotListFetch
	prevPrepare := prepareSnapshotTargets
	prevEnsure := ensureSnapshotCredentials
	prevBilled := fetchSnapshotBilledCosts
	t.Cleanup(func() {
		snapshotListFetch = prevFetch
		prepareSnapshotTargets = prevPrepare
		ensureSnapshotCredentials = prevEnsure
		fetchSnapshotBilledCosts = prevBilled
		awsFlags.ConfigPath = ""
	})

	snapshotListFetch = func(_ context.Context, q snapshot.Query) (snapshot.Result, error) {
		if len(q.Targets) != 1 || q.Targets[0].AccountID != "123456789012" {
			t.Fatalf("targets = %#v", q.Targets)
		}
		if q.OlderThan != 90*24*time.Hour {
			t.Fatalf("older than = %v", q.OlderThan)
		}
		return snapshot.Result{
			Summary: snapshot.Summary{
				TotalCount:              1,
				EstimatedMonthlyCostUSD: 5,
				OlderThanDays:           90,
				CostDisclaimer:          "Estimates use volume or allocated size; actual EBS snapshot billing may be lower.",
			},
		}, nil
	}
	ensureSnapshotCredentials = func(_ context.Context, _ *cobra.Command, _ configstore.File, _ []cost.AccountTarget, _, _, _ string) error {
		return nil
	}
	fetchSnapshotBilledCosts = func(_ context.Context, _ configstore.File, _ []cost.AccountTarget, _ string, _ time.Time) ([]snapshot.AccountBilledSnapshotCosts, error) {
		return nil, nil
	}
	prepareSnapshotTargets = func(_ context.Context, _ *cobra.Command, _ configstore.File, targets []cost.AccountTarget, _, _, _ string, _ costStepper) ([]snapshot.AccountTarget, error) {
		out := make([]snapshot.AccountTarget, 0, len(targets))
		for _, target := range targets {
			out = append(out, snapshot.AccountTarget{AccountID: target.AccountID})
		}
		return out, nil
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
}
