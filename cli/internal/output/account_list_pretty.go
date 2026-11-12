// account_list_pretty.go renders registered accounts as colored tables.
package output

import (
	"fmt"
	"io"

	"github.com/olekukonko/tablewriter"
)

func writeAWSAccountListPretty(w io.Writer, entries []AccountListRow) error {
	s := newStyler(w)
	if len(entries) == 0 {
		return writeAccountListEmpty(w, s, "AWS")
	}
	if err := writeAccountListSummary(w, s, "AWS", entries); err != nil {
		return err
	}
	return writeAWSAccountTable(w, s, entries)
}

func writeSnowflakeAccountListPretty(w io.Writer, entries []AccountListRow) error {
	s := newStyler(w)
	if len(entries) == 0 {
		return writeAccountListEmpty(w, s, "Snowflake")
	}
	if err := writeAccountListSummary(w, s, "Snowflake", entries); err != nil {
		return err
	}
	return writeSnowflakeAccountTable(w, s, entries)
}

func writeGCPAccountListPretty(w io.Writer, entries []AccountListRow) error {
	s := newStyler(w)
	if len(entries) == 0 {
		return writeAccountListEmpty(w, s, "GCP")
	}
	if err := writeAccountListSummary(w, s, "GCP", entries); err != nil {
		return err
	}
	return writeGCPAccountTable(w, s, entries)
}

func writeAccountListEmpty(w io.Writer, s styler, provider string) error {
	msg := fmt.Sprintf("No %s accounts registered.", provider)
	if s.enabled {
		msg = s.dim(msg)
	}
	_, err := fmt.Fprintln(w, msg)
	return err
}

func writeAccountListSummary(w io.Writer, s styler, provider string, entries []AccountListRow) error {
	payers, linked := 0, 0
	for _, e := range entries {
		switch e.Kind {
		case "linked":
			linked++
		case "payer":
			payers++
		}
	}

	label := fmt.Sprintf("Registered %s accounts:", provider)
	value := fmt.Sprintf("%d", len(entries))
	if provider == "AWS" && (payers > 0 || linked > 0) {
		value = fmt.Sprintf("%d (%d payer, %d linked)", len(entries), payers, linked)
	}
	if s.enabled {
		label = s.dim(label)
		value = s.bold(value)
	}
	if _, err := fmt.Fprintf(w, "  %s  %s\n\n", label, value); err != nil {
		return err
	}
	return nil
}

func writeAWSAccountTable(w io.Writer, s styler, entries []AccountListRow) error {
	title := "AWS accounts"
	if s.enabled {
		title = s.bold(s.cyan(title))
	}
	if _, err := fmt.Fprintln(w, title); err != nil {
		return err
	}

	table := newAccountTable(w)
	table.SetHeader([]string{
		cell(s, s.bold, "ALIAS"),
		cell(s, s.bold, "ACCOUNT ID"),
		cell(s, s.bold, "TYPE"),
		cell(s, s.bold, "PAYER"),
		cell(s, s.bold, "ROLE"),
	})
	table.SetColumnAlignment([]int{
		tablewriter.ALIGN_LEFT,
		tablewriter.ALIGN_LEFT,
		tablewriter.ALIGN_LEFT,
		tablewriter.ALIGN_LEFT,
		tablewriter.ALIGN_LEFT,
	})

	for _, e := range entries {
		table.Append([]string{
			cell(s, s.bold, e.Alias),
			e.AccountID,
			formatAccountKind(s, e.Kind),
			formatOptionalField(s, e.PayerAlias),
			formatOptionalField(s, e.Role),
		})
	}
	table.Render()
	return nil
}

func writeGCPAccountTable(w io.Writer, s styler, entries []AccountListRow) error {
	title := "GCP accounts"
	if s.enabled {
		title = s.bold(s.cyan(title))
	}
	if _, err := fmt.Fprintln(w, title); err != nil {
		return err
	}

	table := newAccountTable(w)
	table.SetHeader([]string{
		cell(s, s.bold, "ALIAS"),
		cell(s, s.bold, "ACCOUNT ID"),
	})
	table.SetColumnAlignment([]int{
		tablewriter.ALIGN_LEFT,
		tablewriter.ALIGN_LEFT,
	})

	for _, e := range entries {
		table.Append([]string{
			cell(s, s.bold, e.Alias),
			e.AccountID,
		})
	}
	table.Render()
	return nil
}

func writeSnowflakeAccountTable(w io.Writer, s styler, entries []AccountListRow) error {
	title := "Snowflake accounts"
	if s.enabled {
		title = s.bold(s.cyan(title))
	}
	if _, err := fmt.Fprintln(w, title); err != nil {
		return err
	}

	table := newAccountTable(w)
	table.SetHeader([]string{
		cell(s, s.bold, "ALIAS"),
		cell(s, s.bold, "ACCOUNT"),
		cell(s, s.bold, "ROLE"),
	})
	table.SetColumnAlignment([]int{
		tablewriter.ALIGN_LEFT,
		tablewriter.ALIGN_LEFT,
		tablewriter.ALIGN_LEFT,
	})

	for _, e := range entries {
		table.Append([]string{
			cell(s, s.bold, e.Alias),
			e.AccountID,
			formatOptionalField(s, e.Role),
		})
	}
	table.Render()
	return nil
}

func newAccountTable(w io.Writer) *tablewriter.Table {
	table := tablewriter.NewWriter(w)
	table.SetAutoWrapText(false)
	table.SetBorder(false)
	// Title()/ToUpper would turn SGR "\033[1m" into "\033[1M" (CSI Delete Line) and erase headers.
	table.SetAutoFormatHeaders(false)
	table.SetHeaderAlignment(tablewriter.ALIGN_LEFT)
	table.SetAlignment(tablewriter.ALIGN_LEFT)
	table.SetTablePadding("\t")
	return table
}

func formatAccountKind(s styler, kind string) string {
	if !s.enabled {
		return kind
	}
	switch kind {
	case "payer":
		return s.cyan(kind)
	case "linked":
		return s.yellow(kind)
	default:
		return kind
	}
}

func formatOptionalField(s styler, value string) string {
	if value == "" {
		return formatDash(s)
	}
	return value
}

func formatDash(s styler) string {
	if s.enabled {
		return s.dim("-")
	}
	return "-"
}
