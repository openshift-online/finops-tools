package output

import (
	"fmt"
	"io"
	"strings"

	"github.com/olekukonko/tablewriter"
	"github.com/openshift-online/finops-tools/cli/internal/format"
	"github.com/openshift-online/finops-tools/core/accountreview"
)

const accountDetailsRuleWidth = 72

func writeAccountDetailsPretty(w io.Writer, details []accountreview.AccountDetails) error {
	sanitized := make([]accountreview.AccountDetails, len(details))
	for i, d := range details {
		sanitized[i] = sanitizeAccountDetailsForTTY(d)
	}
	details = sanitized
	s := newStyler(w)
	if len(details) == 0 {
		msg := "No accounts matched the selection."
		if s.enabled {
			msg = s.dim(msg)
		}
		_, err := fmt.Fprintln(w, msg)
		return err
	}
	if len(details) > 1 {
		if err := writeAccountDetailsPrettyOverview(w, s, details); err != nil {
			return err
		}
		if err := writeAccountDetailsSeparator(w, s); err != nil {
			return err
		}
	}
	for i, d := range details {
		if i > 0 {
			if err := writeAccountDetailsSeparator(w, s); err != nil {
				return err
			}
		}
		if err := writeAccountDetailsPrettyAccount(w, s, d, i+1, len(details)); err != nil {
			return err
		}
	}
	return nil
}

func writeAccountDetailsSeparator(w io.Writer, s styler) error {
	if _, err := fmt.Fprintln(w); err != nil {
		return err
	}
	rule := strings.Repeat("═", accountDetailsRuleWidth)
	if s.enabled {
		rule = s.dim(rule)
	}
	if _, err := fmt.Fprintln(w, rule); err != nil {
		return err
	}
	_, err := fmt.Fprintln(w)
	return err
}

func writeAccountDetailsPrettyOverview(w io.Writer, s styler, details []accountreview.AccountDetails) error {
	rows := make([][]string, len(details))
	for i, d := range details {
		last, total := "-", "-"
		if l, t, ok := accountDetailsCostSummary(d); ok {
			last, total = l, t
		} else if s.enabled {
			last, total = s.dim("-"), s.dim("-")
		}
		owner := d.OwnerEmail
		if owner == "" {
			owner = "-"
			if d.OwnerError != "" {
				owner = d.OwnerError
			}
			if s.enabled && owner == "-" {
				owner = s.dim("-")
			}
		}
		rows[i] = []string{d.AccountName, d.AccountID, last, total, owner}
	}
	return writeAccountDetailsTable(w, s, "Accounts in this review", []string{
		"Account", "ID", "Last listed month", "Period total", "Owner",
	}, rows)
}

func writeAccountDetailsPrettyAccount(w io.Writer, s styler, d accountreview.AccountDetails, index, total int) error {
	title := fmt.Sprintf("%s (%s)", d.AccountName, d.AccountID)
	if total > 1 {
		title = fmt.Sprintf("[%d/%d] %s", index, total, title)
	}
	if s.enabled {
		title = s.bold(s.yellow(title))
	}
	if _, err := fmt.Fprintln(w, title); err != nil {
		return err
	}
	rule := strings.Repeat("─", accountDetailsRuleWidth)
	if s.enabled {
		rule = s.dim(rule)
	}
	if _, err := fmt.Fprintln(w, rule); err != nil {
		return err
	}

	var meta []struct{ label, value string }
	if last, total, ok := accountDetailsCostSummary(d); ok {
		meta = append(meta, struct{ label, value string }{"Last listed month", last})
		meta = append(meta, struct{ label, value string }{"Period total", total})
	}
	if d.DisplayAlias != "" {
		meta = append(meta, struct{ label, value string }{"Alias", d.DisplayAlias})
	}
	if d.OUPath != "" {
		meta = append(meta, struct{ label, value string }{"OU", d.OUPath})
	}
	if d.OwnerEmail != "" {
		meta = append(meta, struct{ label, value string }{"Owner", d.OwnerEmail})
	} else if d.OwnerError != "" {
		meta = append(meta, struct{ label, value string }{"Owner", d.OwnerError})
	}
	for _, m := range meta {
		label := m.label + ":"
		if s.enabled {
			label = s.dim(label)
		}
		if _, err := fmt.Fprintf(w, "  %s  %s\n", label, m.value); err != nil {
			return err
		}
	}

	if len(d.Tags) > 0 {
		tagRows := make([][]string, len(d.Tags))
		for i, tag := range d.Tags {
			tagRows[i] = []string{tag.Key, tag.Value}
		}
		if err := writeAccountDetailsTable(w, s, "Tags", []string{"Tag", "Value"}, tagRows); err != nil {
			return err
		}
	}

	if err := writeAccountDetailsPrettyCosts(w, s, d); err != nil {
		return err
	}

	if err := writeSectionTitle(w, s, "Resources"); err != nil {
		return err
	}
	tables := d.NonemptyInventoryTables()
	counts := d.NonzeroInventoryCounts()
	for _, table := range tables {
		section := fmt.Sprintf("%s (%d)", table.Title, table.Count)
		if err := writeSubsectionTitle(w, s, section); err != nil {
			return err
		}
		if err := writeAccountDetailsTable(w, s, "", table.Headers, table.Rows); err != nil {
			return err
		}
	}
	for _, c := range counts {
		if _, err := fmt.Fprintf(w, "  %s: %d\n", c.Title, c.Count); err != nil {
			return err
		}
	}
	if line := d.NoneFoundLine(); line != "" {
		if s.enabled {
			line = s.dim(line)
		}
		if _, err := fmt.Fprintf(w, "  %s\n", line); err != nil {
			return err
		}
	}

	if d.InventoryError != "" {
		msg := "Inventory warnings: " + d.InventoryError
		if s.enabled {
			msg = s.dim(msg)
		}
		if _, err := fmt.Fprintf(w, "\n%s\n", msg); err != nil {
			return err
		}
	}
	return nil
}

func writeAccountDetailsPrettyCosts(w io.Writer, s styler, d accountreview.AccountDetails) error {
	if err := writeSectionTitle(w, s, "Monthly costs (net amortized)"); err != nil {
		return err
	}
	mc := d.MonthlyCosts
	if mc.Error != "" {
		msg := "(unavailable: " + mc.Error + ")"
		if s.enabled {
			msg = s.dim(msg)
		}
		_, err := fmt.Fprintf(w, "  %s\n", msg)
		return err
	}
	if len(mc.Months) == 0 {
		msg := "(no data)"
		if s.enabled {
			msg = s.dim(msg)
		}
		if _, err := fmt.Fprintf(w, "  %s\n", msg); err != nil {
			return err
		}
	} else {
		rows := make([][]string, 0, len(mc.Months)+1)
		for _, m := range mc.Months {
			rows = append(rows, []string{m.Month, format.FormatMoney(m.Amount, mc.Currency)})
		}
		rows = append(rows, []string{"Total", format.FormatMoney(mc.Total, mc.Currency)})
		if err := writeAccountDetailsTable(w, s, "", []string{"Month", "Amount"}, rows); err != nil {
			return err
		}
	}

	if len(mc.TopServices) > 0 {
		rows := make([][]string, len(mc.TopServices))
		for i, svc := range mc.TopServices {
			rows[i] = []string{svc.Service, format.FormatMoney(svc.Amount, mc.Currency)}
		}
		return writeAccountDetailsTable(w, s, "Top services (last complete month)", []string{"Service", "Amount"}, rows)
	}
	if mc.TopServicesError != "" {
		if err := writeSectionTitle(w, s, "Top services (last complete month)"); err != nil {
			return err
		}
		msg := "(unavailable: " + mc.TopServicesError + ")"
		if s.enabled {
			msg = s.dim(msg)
		}
		_, err := fmt.Fprintf(w, "  %s\n", msg)
		return err
	}
	return nil
}

func accountDetailsCostSummary(d accountreview.AccountDetails) (last, total string, ok bool) {
	mc := d.MonthlyCosts
	if mc.Error != "" || len(mc.Months) == 0 {
		return "", "", false
	}
	m := mc.Months[len(mc.Months)-1]
	return format.FormatMoney(m.Amount, mc.Currency), format.FormatMoney(mc.Total, mc.Currency), true
}

func writeSectionTitle(w io.Writer, s styler, title string) error {
	if title == "" {
		return nil
	}
	if _, err := fmt.Fprintln(w); err != nil {
		return err
	}
	if s.enabled {
		// Yellow bold is reserved for account banners; sections stay bold only.
		title = s.bold(title)
	}
	_, err := fmt.Fprintln(w, title)
	return err
}

func writeSubsectionTitle(w io.Writer, s styler, title string) error {
	if title == "" {
		return nil
	}
	if _, err := fmt.Fprintln(w); err != nil {
		return err
	}
	if s.enabled {
		title = s.dim(title)
	}
	_, err := fmt.Fprintf(w, "  %s\n", title)
	return err
}

func writeAccountDetailsTable(w io.Writer, s styler, title string, headers []string, rows [][]string) error {
	if title != "" {
		if err := writeSectionTitle(w, s, title); err != nil {
			return err
		}
	}
	if len(rows) == 0 {
		return nil
	}
	table := tablewriter.NewWriter(w)
	table.SetAutoWrapText(false)
	table.SetBorder(false)
	// Title()/ToUpper would turn SGR "\033[1m" into "\033[1M" (CSI Delete Line) and erase headers.
	table.SetAutoFormatHeaders(false)
	table.SetHeaderAlignment(tablewriter.ALIGN_LEFT)
	table.SetAlignment(tablewriter.ALIGN_LEFT)
	table.SetTablePadding("\t")
	header := make([]string, len(headers))
	align := make([]int, len(headers))
	for i, h := range headers {
		header[i] = cell(s, s.bold, strings.ToUpper(h))
		align[i] = tablewriter.ALIGN_LEFT
	}
	table.SetHeader(header)
	table.SetColumnAlignment(align)
	for _, row := range rows {
		table.Append(row)
	}
	table.Render()
	return nil
}
