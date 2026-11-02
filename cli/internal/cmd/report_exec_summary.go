// report_exec_summary.go wires the exec-summary report template into "finops report create".
package cmd

import (
	"fmt"

	"github.com/openshift-online/finops-tools/core/cost"
	"github.com/spf13/cobra"
)

// validateExecSummarySelection enforces the exec-summary report's scoped options:
// a single --payer alias and at most the shared --months period flag.
func validateExecSummarySelection(cmd *cobra.Command, sel costTargetSelector) error {
	if sel.PayerAlias == "" {
		return fmt.Errorf("exec-summary report requires --payer <alias>")
	}
	if len(sel.AccountIDs) > 0 || len(sel.Aliases) > 0 || len(sel.OUs) > 0 || sel.TagKey != "" {
		return fmt.Errorf("exec-summary report selects accounts with --payer alone (not --account-id/--account-alias, --ou, or --tag)")
	}
	for _, flag := range []string{"days", "from", "to", "exclude-recent-days"} {
		if cmd.Flags().Changed(flag) {
			return fmt.Errorf("exec-summary report supports only --months as its period flag (completed calendar months), not --%s", flag)
		}
	}
	return nil
}

// buildExecSummaryTargets prepends the payer's own target to the linked member
// targets so the report covers payer own spend plus active members exactly once.
func buildExecSummaryTargets(payerID, payerAlias string, memberTargets []cost.AccountTarget) []cost.AccountTarget {
	payer := cost.AccountTarget{
		AccountID:    payerID,
		DisplayAlias: payerAlias,
	}
	return mergeCostTargets([]cost.AccountTarget{payer}, memberTargets)
}
