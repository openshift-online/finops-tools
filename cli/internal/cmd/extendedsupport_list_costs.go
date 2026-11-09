package cmd

import (
	"context"
	"fmt"
	"sort"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/openshift-online/finops-tools/cli/internal/configstore"
	"github.com/openshift-online/finops-tools/core/cost"
	"github.com/openshift-online/finops-tools/core/extendedsupport"
	"github.com/spf13/cobra"
)

var (
	extendedSupportListEnrichCosts             = enrichExtendedSupportListCostsImpl
	fetchExtendedSupportListCosts              = extendedsupport.EnrichListCosts
	loadExtendedSupportListCostConfig          = loadAWSConfigForCredentialsAccount
	extendedSupportListCheckCharges            = checkExtendedSupportChargesWithoutFindingsImpl
	fetchExtendedSupportChargesWithoutFindings = extendedsupport.FindChargesWithoutFindings
)

// Cost summaries are enriched after scan retries using payer/direct-account
// billing credentials independently of resource-discovery member roles.
func enrichExtendedSupportListCostsImpl(cmd *cobra.Command, cfg configstore.File, targets []cost.AccountTarget, scan extendedsupport.Query, result extendedsupport.Result) (extendedsupport.Result, error) {
	ctx := awsCommandContext(cmd)
	needed := make(map[string]bool)
	for _, record := range result.Records {
		needed[record.AccountID] = true
	}
	var selected []cost.AccountTarget
	for _, target := range targets {
		if needed[target.AccountID] {
			selected = append(selected, target)
		}
	}
	billingTargets, warnings, err := loadExtendedSupportBillingTargets(ctx, cfg, selected)
	if err != nil {
		return result, err
	}
	result, err = fetchExtendedSupportListCosts(ctx, extendedsupport.ListCostQuery{
		Targets: billingTargets, Now: scan.Now, Services: scan.Services, Regions: scan.Regions, SkipRegions: scan.SkipRegions,
	}, result)
	if err != nil {
		return result, err
	}
	result.Summary.Costs.Warnings = append(result.Summary.Costs.Warnings, warnings...)
	return result, nil
}

func checkExtendedSupportChargesWithoutFindingsImpl(cmd *cobra.Command, cfg configstore.File, targets []cost.AccountTarget, scan extendedsupport.Query, result extendedsupport.Result) (extendedsupport.Result, error) {
	candidates := extendedsupport.AccountsWithoutFindings(targets, result)
	if len(candidates) == 0 {
		return result, nil
	}
	ctx := awsCommandContext(cmd)
	billingTargets, warnings, err := loadExtendedSupportBillingTargets(ctx, cfg, candidates)
	if err != nil {
		return result, err
	}
	result, err = fetchExtendedSupportChargesWithoutFindings(ctx, extendedsupport.ListCostQuery{
		Targets: billingTargets, Now: scan.Now, Regions: scan.Regions, SkipRegions: scan.SkipRegions,
	}, result)
	if err != nil {
		return result, err
	}
	metadata := result.Summary.ChargesWithoutFindings
	// Retain the complete attempted scope, including credential-load failures.
	metadata.AccountIDs = nil
	for _, candidate := range candidates {
		metadata.AccountIDs = append(metadata.AccountIDs, candidate.AccountID)
	}
	sort.Strings(metadata.AccountIDs)
	metadata.Warnings = append(metadata.Warnings, warnings...)
	return result, nil
}

func loadExtendedSupportBillingTargets(ctx context.Context, cfg configstore.File, targets []cost.AccountTarget) ([]cost.AccountTarget, []string, error) {
	configs := make(map[string]aws.Config)
	failures := make(map[string]error)
	var warnings []string
	var billingTargets []cost.AccountTarget
	for _, target := range targets {
		id := target.CredentialsAccountID()
		if _, ok := configs[id]; !ok && failures[id] == nil {
			// Inventory preparation already ensured the billing credentials. Load
			// the payer/direct profile, never a member discovery role.
			var err error
			configs[id], err = loadExtendedSupportListCostConfig(ctx, cfg, id, awsFlags.CredentialsFile)
			if err != nil {
				delete(configs, id)
				failures[id] = err
				warnings = append(warnings, fmt.Sprintf("billing source %s configuration unavailable: %v", id, err))
			}
		}
		if failures[id] != nil {
			continue
		}
		target.AWSConfig = configs[id]
		billingTargets = append(billingTargets, target)
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	return billingTargets, warnings, nil
}
