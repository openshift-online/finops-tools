// aws_targets.go registers shared AWS account target flags for get-cost, snapshot list, and report create.
package cmd

import (
	"fmt"

	"github.com/openshift-online/finops-tools/core/parallel"
	"github.com/spf13/cobra"
)

type awsTargetFlagRefs struct {
	Account         *string
	AccountAliases  *string
	OU              *string
	Payer           *string
	Tag             *string
	SkipOrgCache    *bool
	RefreshOrgCache *bool
}

func bindAWSTargetFlags(cmd *cobra.Command, refs awsTargetFlagRefs) {
	cmd.Flags().StringVar(refs.Account, "account-id", "", "Provider-native AWS account ID(s), comma-separated 12-digit IDs")
	cmd.Flags().StringVar(refs.AccountAliases, "account-alias", "", "Configured AWS account alias(es), comma-separated")
	cmd.Flags().StringVar(refs.OU, "ou", "", "AWS OU or org-root ID(s), comma-separated (requires --payer). Scope per ID: bare=subtree, /=direct accounts, /*=one child-OU level (quote in shells), /**=subtree")
	cmd.Flags().StringVar(refs.Payer, "payer", "", "Registered payer alias: alone selects all org members; also required with --ou/--tag or unregistered --account-id values")
	cmd.Flags().StringVar(refs.Tag, "tag", "", "Select accounts by Organizations tag: KEY or KEY=VALUE (requires --payer)")
	if refs.SkipOrgCache != nil {
		cmd.Flags().BoolVar(refs.SkipOrgCache, "skip-org-cache", false, "Bypass cached organization account/tag data (always fetch live from AWS)")
	}
	if refs.RefreshOrgCache != nil {
		cmd.Flags().BoolVar(refs.RefreshOrgCache, "refresh-org-cache", false, "Ignore cached organization data and refresh the cache from AWS")
	}
}

type awsAccountSelectorFlagRefs struct {
	Payer     *string
	Alias     *string
	AccountID *string
}

func bindAWSAccountSelectorFlags(cmd *cobra.Command, refs awsAccountSelectorFlagRefs, payerHelp string) {
	cmd.Flags().StringVar(refs.Payer, "payer", "", payerHelp)
	cmd.Flags().StringVar(refs.Alias, "account-alias", "", "Registered account alias")
	cmd.Flags().StringVar(refs.AccountID, "account-id", "", "12-digit AWS account ID")
}

// bindWorkersFlag registers --workers for bounded parallel AWS queries when multiple accounts are selected.
// usagePrefix is prepended to the flag description (e.g. "costs template only; ").
func bindWorkersFlag(cmd *cobra.Command, workers *int, usagePrefix string) {
	cmd.Flags().IntVar(workers, "workers", parallel.DefaultWorkers,
		fmt.Sprintf("%sMaximum concurrent workers for multi-account AWS queries (default %d, max %d; use 1 for sequential)",
			usagePrefix, parallel.DefaultWorkers, parallel.MaxWorkers))
}

func validateWorkers(workers int) error {
	if workers < 1 {
		return fmt.Errorf("--workers must be at least 1")
	}
	if workers > parallel.MaxWorkers {
		return fmt.Errorf("--workers must be at most %d", parallel.MaxWorkers)
	}
	return nil
}
