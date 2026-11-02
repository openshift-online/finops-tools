package cmd

import (
	"context"
	"reflect"
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
	fetchBilledSnapshotCostsForGroup = func(_ context.Context, _ snapshot.CostExplorerAPI, accountIDs []string, _ time.Time, _ int) ([]snapshot.AccountBilledSnapshotCosts, error) {
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

	got, err := fetchSnapshotBilledCostsImpl(context.Background(), configstore.File{}, targets, "", time.Now().UTC(), 1)
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

func TestRegionalSnapshotBillingUsesPayerCredentialsAndExactAccountScopes(t *testing.T) {
	origLoad := loadAWSConfigForCredentialsAccount
	origRegional := fetchRegionalBilledSnapshotCostsForGroup
	origWide := fetchBilledSnapshotCostsForGroup
	t.Cleanup(func() {
		loadAWSConfigForCredentialsAccount = origLoad
		fetchRegionalBilledSnapshotCostsForGroup = origRegional
		fetchBilledSnapshotCostsForGroup = origWide
	})
	var credentials []string
	loadAWSConfigForCredentialsAccount = func(_ context.Context, _ configstore.File, id, _ string) (aws.Config, error) {
		credentials = append(credentials, id)
		return aws.Config{}, nil
	}
	fetchBilledSnapshotCostsForGroup = func(context.Context, snapshot.CostExplorerAPI, []string, time.Time, int) ([]snapshot.AccountBilledSnapshotCosts, error) {
		t.Fatal("regional billing must not call account-wide billing")
		return nil, nil
	}
	var scopesSeen []snapshot.AccountScanScope
	fetchRegionalBilledSnapshotCostsForGroup = func(_ context.Context, _ snapshot.CostExplorerAPI, scopes []snapshot.AccountScanScope, _ time.Time, _ int) ([]snapshot.AccountBilledSnapshotCosts, error) {
		scopesSeen = append(scopesSeen, scopes...)
		var out []snapshot.AccountBilledSnapshotCosts
		for _, scope := range scopes {
			out = append(out, snapshot.AccountBilledSnapshotCosts{AccountID: scope.AccountID})
		}
		return out, nil
	}
	targets := []cost.AccountTarget{
		{AccountID: "111111111111", PayerAccountID: "999999999999"},
		{AccountID: "222222222222", PayerAccountID: "999999999999"},
		{AccountID: "333333333333", PayerAccountID: "888888888888"},
	}
	scopes := []snapshot.AccountScanScope{
		{AccountID: "111111111111", Regions: []string{"us-east-1"}},
		{AccountID: "333333333333", Regions: []string{"eu-west-1"}},
	}
	got, err := fetchRegionalSnapshotBilledCostsImpl(context.Background(), configstore.File{}, targets, "", scopes, time.Now(), 1)
	if err != nil {
		t.Fatal(err)
	}
	wantScopes := []snapshot.AccountScanScope{scopes[0], {AccountID: "222222222222"}, scopes[1]}
	if len(got) != 3 || !reflect.DeepEqual(scopesSeen, wantScopes) || !reflect.DeepEqual(credentials, []string{"999999999999", "888888888888"}) {
		t.Fatalf("billing accounts=%v scopes=%+v credentials=%v", got, scopesSeen, credentials)
	}
	// No scan metadata must become empty regional scopes, never account-wide.
	scopesSeen = nil
	_, err = fetchRegionalSnapshotBilledCostsImpl(context.Background(), configstore.File{}, targets, "", nil, time.Now(), 1)
	if err != nil || len(scopesSeen) != 3 {
		t.Fatalf("empty scopes=%v err=%v", scopesSeen, err)
	}
	for _, scope := range scopesSeen {
		if len(scope.Regions) != 0 {
			t.Fatalf("unexpected scan regions: %+v", scope)
		}
	}
}
