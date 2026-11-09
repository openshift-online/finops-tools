package cmd

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/openshift-online/finops-tools/cli/internal/configstore"
	"github.com/openshift-online/finops-tools/core/cost"
	"github.com/openshift-online/finops-tools/core/extendedsupport"
	"github.com/spf13/cobra"
)

func TestExtendedSupportListIncludesCostsWithoutTemporaryFlags(t *testing.T) {
	for _, name := range []string{"with-costs", "get-costs"} {
		if extendedSupportListCmd.Flags().Lookup(name) != nil {
			t.Fatalf("temporary flag --%s still exists", name)
		}
	}
	if !strings.Contains(extendedSupportListCmd.Long, "The list includes historical Extended Support charges") {
		t.Fatal("default cost behavior is not documented")
	}
}

func TestExtendedSupportListCostsLoadBillingSourcesOnceAndKeepInventoryOnFailure(t *testing.T) {
	previousLoad, previousFetch := loadExtendedSupportListCostConfig, fetchExtendedSupportListCosts
	t.Cleanup(func() {
		loadExtendedSupportListCostConfig, fetchExtendedSupportListCosts = previousLoad, previousFetch
	})
	var loaded []string
	loadExtendedSupportListCostConfig = func(_ context.Context, _ configstore.File, id, _ string) (aws.Config, error) {
		loaded = append(loaded, id)
		if id == "987654321098" {
			return aws.Config{}, errors.New("expired billing credentials")
		}
		return aws.Config{Region: "billing"}, nil
	}
	now := time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)
	query := extendedsupport.Query{Now: now, Services: []extendedsupport.Service{extendedsupport.ServiceRDS}, Regions: []string{"us-east-1"}, SkipRegions: []string{"us-west-2"}}
	targets := []cost.AccountTarget{
		{AccountID: "111111111111", PayerAccountID: "123456789012"},
		{AccountID: "222222222222", PayerAccountID: "123456789012"},
		{AccountID: "333333333333", PayerAccountID: "987654321098"},
		{AccountID: "444444444444", PayerAccountID: "987654321098"},
		{AccountID: "555555555555", PayerAccountID: "555555555555"}, // no findings: no billing credential lookup
	}
	var inventory extendedsupport.Result
	for _, target := range targets[:4] {
		inventory.Records = append(inventory.Records, extendedsupport.Record{AccountID: target.AccountID, ResourceID: "old"})
	}
	inventory.Summary.TotalCount = 4
	fetchExtendedSupportListCosts = func(_ context.Context, q extendedsupport.ListCostQuery, result extendedsupport.Result) (extendedsupport.Result, error) {
		if len(q.Targets) != 2 || q.Targets[0].AWSConfig.Region != "billing" || q.Targets[1].AWSConfig.Region != "billing" || !q.Now.Equal(now) || !reflect.DeepEqual(q.Services, query.Services) || !reflect.DeepEqual(q.Regions, query.Regions) || !reflect.DeepEqual(q.SkipRegions, query.SkipRegions) {
			t.Fatalf("billing query=%+v", q)
		}
		result.Summary.Costs = &extendedsupport.ListCostSummary{}
		return result, nil
	}
	result, err := enrichExtendedSupportListCostsImpl(&cobra.Command{}, configstore.File{}, targets, query, inventory)
	if err != nil || !reflect.DeepEqual(loaded, []string{"123456789012", "987654321098"}) || result.Summary.TotalCount != 4 || len(result.Records) != 4 || len(result.Summary.Costs.Warnings) != 1 || !strings.Contains(result.Summary.Costs.Warnings[0], "expired billing credentials") {
		t.Fatalf("result=%+v loaded=%v err=%v", result, loaded, err)
	}
}

func TestExtendedSupportListCostsEmptyInventoryAndCancellation(t *testing.T) {
	previousLoad := loadExtendedSupportListCostConfig
	t.Cleanup(func() { loadExtendedSupportListCostConfig = previousLoad })
	loadExtendedSupportListCostConfig = func(context.Context, configstore.File, string, string) (aws.Config, error) {
		t.Fatal("empty inventory loaded credentials")
		return aws.Config{}, nil
	}
	result, err := enrichExtendedSupportListCostsImpl(&cobra.Command{}, configstore.File{}, []cost.AccountTarget{{AccountID: "111111111111"}}, extendedsupport.Query{}, extendedsupport.Result{})
	if err != nil || result.Summary.Costs == nil || len(result.Summary.Costs.AccountIDs) != 0 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	cmd := &cobra.Command{}
	cmd.SetContext(ctx)
	_, err = enrichExtendedSupportListCostsImpl(cmd, configstore.File{}, nil, extendedsupport.Query{}, extendedsupport.Result{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation=%v", err)
	}
}

func TestChargesWithoutFindingsLoadOnlyCandidateBillingSources(t *testing.T) {
	prevLoad, prevFetch := loadExtendedSupportListCostConfig, fetchExtendedSupportChargesWithoutFindings
	t.Cleanup(func() {
		loadExtendedSupportListCostConfig, fetchExtendedSupportChargesWithoutFindings = prevLoad, prevFetch
	})
	var loaded []string
	loadExtendedSupportListCostConfig = func(_ context.Context, _ configstore.File, id, _ string) (aws.Config, error) {
		loaded = append(loaded, id)
		if id == "987654321098" {
			return aws.Config{}, errors.New("billing credentials unavailable")
		}
		return aws.Config{Region: "payer-billing"}, nil
	}
	targets := []cost.AccountTarget{
		{AccountID: "111111111111", PayerAccountID: "123456789012"},
		{AccountID: "222222222222", PayerAccountID: "123456789012"},
		{AccountID: "333333333333", PayerAccountID: "123456789012"},
		{AccountID: "444444444444", PayerAccountID: "123456789012"},
		{AccountID: "555555555555", PayerAccountID: "987654321098"},
	}
	result := extendedsupport.Result{Records: []extendedsupport.Record{{AccountID: "111111111111"}}, Summary: extendedsupport.Summary{TotalCount: 1, SkippedAccounts: []extendedsupport.AccountWarning{{AccountID: "222222222222"}}}}
	scan := extendedsupport.Query{Now: time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC), Services: []extendedsupport.Service{extendedsupport.ServiceRDS}, Regions: []string{"us-east-1"}, SkipRegions: []string{"us-west-2"}}
	fetchCalls := 0
	fetchExtendedSupportChargesWithoutFindings = func(_ context.Context, q extendedsupport.ListCostQuery, r extendedsupport.Result) (extendedsupport.Result, error) {
		fetchCalls++
		if len(q.Targets) != 2 || q.Targets[0].AccountID != "333333333333" || q.Targets[1].AccountID != "444444444444" || q.Targets[0].AWSConfig.Region != "payer-billing" || !q.Now.Equal(scan.Now) || len(q.Services) != 0 || !reflect.DeepEqual(q.Regions, scan.Regions) || !reflect.DeepEqual(q.SkipRegions, scan.SkipRegions) {
			t.Fatalf("query=%+v", q)
		}
		r.Summary.ChargesWithoutFindings = &extendedsupport.ChargesWithoutFindingsSummary{Accounts: []extendedsupport.AccountChargesWithoutFindings{}}
		return r, nil
	}
	got, err := checkExtendedSupportChargesWithoutFindingsImpl(&cobra.Command{}, configstore.File{}, targets, scan, result)
	if err != nil || fetchCalls != 1 || !reflect.DeepEqual(loaded, []string{"123456789012", "987654321098"}) || got.Summary.TotalCount != 1 || !reflect.DeepEqual(got.Summary.ChargesWithoutFindings.AccountIDs, []string{"333333333333", "444444444444", "555555555555"}) || len(got.Summary.ChargesWithoutFindings.Warnings) != 1 {
		t.Fatalf("result=%+v loaded=%v err=%v", got.Summary, loaded, err)
	}
	// Accounts with findings or an unresolved scan failure need no extra billing.
	loaded = nil
	got, err = checkExtendedSupportChargesWithoutFindingsImpl(&cobra.Command{}, configstore.File{}, targets[:2], scan, result)
	if err != nil || len(loaded) != 0 || fetchCalls != 1 || got.Summary.ChargesWithoutFindings != nil {
		t.Fatalf("unnecessary query: result=%+v calls=%d err=%v", got.Summary, fetchCalls, err)
	}
}

func TestChargesWithoutFindingsKeepsCredentialFailuresVisible(t *testing.T) {
	prevLoad := loadExtendedSupportListCostConfig
	t.Cleanup(func() { loadExtendedSupportListCostConfig = prevLoad })
	loadExtendedSupportListCostConfig = func(context.Context, configstore.File, string, string) (aws.Config, error) {
		return aws.Config{}, errors.New("expired session")
	}
	got, err := checkExtendedSupportChargesWithoutFindingsImpl(&cobra.Command{}, configstore.File{}, []cost.AccountTarget{{AccountID: "111111111111"}}, extendedsupport.Query{}, extendedsupport.Result{})
	if err != nil || got.Summary.ChargesWithoutFindings == nil || len(got.Summary.ChargesWithoutFindings.Accounts) != 0 || len(got.Summary.ChargesWithoutFindings.Warnings) != 1 || !strings.Contains(got.Summary.ChargesWithoutFindings.Warnings[0], "expired session") {
		t.Fatalf("result=%+v err=%v", got.Summary, err)
	}
}
