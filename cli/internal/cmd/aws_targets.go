// aws_targets.go registers shared AWS account target flags for get-cost, snapshot list, and report create.
package cmd

import "github.com/spf13/cobra"

type awsTargetFlagRefs struct {
	Account         *string
	AccountAliases  *string
	OU              *string
	OUDirect        *bool
	Payer           *string
	TagKey          *string
	TagValue        *string
	SkipOrgCache    *bool
	RefreshOrgCache *bool
}

func bindAWSTargetFlags(cmd *cobra.Command, refs awsTargetFlagRefs) {
	cmd.Flags().StringVar(refs.Account, "account", "", "Payer AWS account ID(s), comma-separated 12-digit IDs")
	cmd.Flags().StringVar(refs.AccountAliases, "account-alias", "", "Configured AWS account alias(es), comma-separated")
	cmd.Flags().StringVar(refs.OU, "ou", "", "AWS OU ID(s), comma-separated (requires --payer; recursive by default)")
	cmd.Flags().BoolVar(refs.OUDirect, "ou-direct", false, "Include only accounts directly in --ou, not descendant OUs")
	cmd.Flags().StringVar(refs.Payer, "payer", "", "Registered payer alias for --account member IDs, --ou, or --tag-key (e.g. rhc)")
	cmd.Flags().StringVar(refs.TagKey, "tag-key", "", "Select accounts by AWS Organizations tag key")
	cmd.Flags().StringVar(refs.TagValue, "tag-value", "", "Optional tag value (omit to match any value for --tag-key)")
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
