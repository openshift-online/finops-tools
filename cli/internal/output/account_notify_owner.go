package output

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/olekukonko/tablewriter"
	"github.com/openshift-online/finops-tools/core/accountreview"
)

// NotifySummary holds delivery outcomes for account owner notifications.
type NotifySummary struct {
	Planned int                            `json:"planned"`
	Sent    int                            `json:"sent"`
	Failed  int                            `json:"failed"`
	Skipped int                            `json:"skipped"`
	Results []accountreview.DeliveryResult `json:"results"`
}

// WriteNotifySummary renders the delivery summary.
func WriteNotifySummary(w io.Writer, format Format, summary NotifySummary) error {
	switch format {
	case FormatPrettyPrint:
		return writeNotifySummaryPretty(w, summary)
	case FormatJSON:
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(summary)
	default:
		return fmt.Errorf("notify summary supports pretty-print and json only")
	}
}

func writeNotifySummaryPretty(w io.Writer, summary NotifySummary) error {
	s := newStyler(w)
	_, _ = fmt.Fprintf(w, "\n%s\n", s.bold(s.cyan("Delivery summary")))
	_, _ = fmt.Fprintf(w, "  Planned:     %s\n", s.bold(fmt.Sprintf("%d", summary.Planned)))
	_, _ = fmt.Fprintf(w, "  Sent:        %s\n", s.bold(fmt.Sprintf("%d", summary.Sent)))
	_, _ = fmt.Fprintf(w, "  Failed:      %s\n", s.bold(fmt.Sprintf("%d", summary.Failed)))
	_, _ = fmt.Fprintf(w, "  Skipped:     %s\n", s.bold(fmt.Sprintf("%d", summary.Skipped)))

	if len(summary.Results) == 0 {
		_, _ = fmt.Fprintln(w, s.dim("  (no delivery results)"))
		return nil
	}

	_, _ = fmt.Fprintf(w, "\n%s\n", s.bold(s.cyan("Details")))
	table := tablewriter.NewWriter(w)
	table.SetAutoWrapText(false)
	table.SetBorder(false)
	table.SetHeaderAlignment(tablewriter.ALIGN_LEFT)
	table.SetAlignment(tablewriter.ALIGN_LEFT)
	table.SetTablePadding("\t")
	table.SetHeader([]string{
		cell(s, s.bold, "ACCOUNT"),
		cell(s, s.bold, "OWNER"),
		cell(s, s.bold, "STATUS"),
		cell(s, s.bold, "REASON"),
	})
	for _, r := range summary.Results {
		account := r.AccountID
		if len(r.AccountIDs) > 0 {
			account = strings.Join(r.AccountIDs, ", ")
		}
		reason := r.Reason
		if reason == "" {
			reason = s.dim("-")
		}
		owner := r.OwnerEmail
		if owner == "" {
			owner = s.dim("-")
		}
		table.Append([]string{
			account,
			owner,
			string(r.Status),
			reason,
		})
	}
	table.Render()
	return nil
}
