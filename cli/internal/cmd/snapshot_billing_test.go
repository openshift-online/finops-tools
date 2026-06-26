package cmd

import (
	"context"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/openshift-online/finops-tools/cli/internal/configstore"
	"github.com/openshift-online/finops-tools/core/cost"
	"github.com/openshift-online/finops-tools/core/snapshot"
)

func TestFetchSnapshotBilledCostsDeduplicatesAccountsPerGroup(t *testing.T) {
	origLoad := loadAWSConfigForCredentialsAccount
	origFetch := fetchBilledSnapshotCostsForGroup
	t.Cleanup(func() {
		loadAWSConfigForCredentialsAccount = origLoad
		fetchBilledSnapshotCostsForGroup = origFetch
	})

	loadAWSConfigForCredentialsAccount = func(context.Context, configstore.File, string, string) (aws.Config, error) {
		return aws.Config{}, nil
	}

	var fetchedAccountIDs []string
	fetchBilledSnapshotCostsForGroup = func(_ context.Context, _ snapshot.CostExplorerAPI, accountIDs []string, _ time.Time) ([]snapshot.AccountBilledSnapshotCosts, error) {
		fetchedAccountIDs = append(fetchedAccountIDs, accountIDs...)
		out := make([]snapshot.AccountBilledSnapshotCosts, 0, len(accountIDs))
		for _, accountID := range accountIDs {
			out = append(out, snapshot.AccountBilledSnapshotCosts{AccountID: accountID})
		}
		return out, nil
	}

	targets := []cost.AccountTarget{
		{AccountID: "111111111111"},
		{AccountID: "222222222222"},
		{AccountID: "111111111111"},
		{AccountID: "222222222222"},
	}

	got, err := fetchSnapshotBilledCostsImpl(context.Background(), configstore.File{}, targets, "", time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if len(fetchedAccountIDs) != 2 {
		t.Fatalf("fetched account IDs = %v, want 2 unique", fetchedAccountIDs)
	}
	if fetchedAccountIDs[0] != "111111111111" || fetchedAccountIDs[1] != "222222222222" {
		t.Fatalf("fetched account IDs = %v", fetchedAccountIDs)
	}
	if len(got) != 2 {
		t.Fatalf("result rows = %d, want 2", len(got))
	}
	if got[0].AccountID != "111111111111" || got[1].AccountID != "222222222222" {
		t.Fatalf("result order = %#v", got)
	}
}
