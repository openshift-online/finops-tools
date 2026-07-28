// cost_pretty.go renders cost summaries as tables with proportional bar charts for service/account splits.
package output

import (
	"fmt"
	"io"
	"math"
	"strings"
	"time"

	"github.com/olekukonko/tablewriter"
	"github.com/openshift-online/finops-tools/core/cost"
)

const (
	barWidth       = 24
	maxServiceLen  = 44
)

func writePrettyPrint(w io.Writer, r cost.CostResult) error {
	s := newStyler(w)
	if err := writePrettySummary(w, s, r); err != nil {
		return err
	}
	if len(r.Breakdown) == 0 {
		return nil
	}
	return writePrettyBreakdown(w, s, r)
}

func writePrettySummary(w io.Writer, s styler, r cost.CostResult) error {
	amount := formatAmount(r.Amount)
	totalLine := fmt.Sprintf("%s %s", r.Currency, amount)
	if s.enabled {
		totalLine = s.bold(s.yellow(totalLine))
	}

	accountLabel := accountSummaryLabel(r)
	lines := []struct{ label, value string }{
		{netAmortizedCostLabel(r.StartDate, r.EndDate), totalLine},
		{accountLabel, formatAccountList(r.AccountName, r.AccountID)},
		{"Period", fmt.Sprintf("%s – %s", r.StartDate, r.EndDate)},
	}
	if r.SplitBy != cost.SplitByNone {
		lines = append(lines, struct{ label, value string }{"Split by", string(r.SplitBy)})
	}

	labelWidth := 0
	for _, ln := range lines {
		if len(ln.label) > labelWidth {
			labelWidth = len(ln.label)
		}
	}

	for _, ln := range lines {
		label := ln.label
		if s.enabled {
			label = s.dim(label)
		}
		if _, err := fmt.Fprintf(w, "  %-*s  %s\n", labelWidth, label+":", ln.value); err != nil {
			return err
		}
	}
	return nil
}

func writePrettyBreakdown(w io.Writer, s styler, r cost.CostResult) error {
	if _, err := fmt.Fprintln(w); err != nil {
		return err
	}

	title, keyHeader := breakdownTitles(r.SplitBy)
	if s.enabled {
		title = s.bold(s.cyan(title))
	}
	if _, err := fmt.Fprintln(w, title); err != nil {
		return err
	}

	maxAmount := r.Amount
	if r.SplitBy == cost.SplitByOU {
		maxAmount = 0
		for _, item := range r.Breakdown {
			if item.Amount > maxAmount {
				maxAmount = item.Amount
			}
		}
	}
	if maxAmount <= 0 && len(r.Breakdown) > 0 {
		maxAmount = r.Breakdown[0].Amount
	}
	if maxAmount <= 0 {
		maxAmount = 1
	}

	table := tablewriter.NewWriter(w)
	table.SetAutoWrapText(false)
	table.SetBorder(false)
	table.SetHeaderAlignment(tablewriter.ALIGN_LEFT)
	table.SetAlignment(tablewriter.ALIGN_LEFT)
	table.SetColumnAlignment([]int{
		tablewriter.ALIGN_LEFT,
		tablewriter.ALIGN_RIGHT,
		tablewriter.ALIGN_RIGHT,
		tablewriter.ALIGN_LEFT,
	})
	table.SetHeader([]string{
		cell(s, s.bold, keyHeader),
		cell(s, s.bold, "AMOUNT"),
		cell(s, s.bold, "SHARE"),
		cell(s, s.bold, "RELATIVE COST"),
	})
	table.SetTablePadding("\t")

	for i, item := range r.Breakdown {
		share := 0.0
		if r.Amount > 0 {
			share = item.Amount / r.Amount * 100
		}
		amountStr := fmt.Sprintf("%s %s", r.Currency, formatAmount(item.Amount))
		if s.enabled && i == 0 {
			amountStr = s.bold(amountStr)
		}
		label := item.DisplayLabel(r.SplitBy)
		if r.SplitBy == cost.SplitByOU && item.OUDepth > 0 {
			label = strings.Repeat("  ", item.OUDepth) + label
		}
		table.Append([]string{
			truncateLabel(label),
			amountStr,
			fmt.Sprintf("%.1f%%", share),
			renderBar(s, item.Amount/maxAmount, i),
		})
	}

	table.Render()
	return nil
}

func netAmortizedCostLabel(startDate, endDate string) string {
	start, err1 := time.Parse("2006-01-02", startDate)
	end, err2 := time.Parse("2006-01-02", endDate)
	if err1 != nil || err2 != nil {
		return "Net amortized cost"
	}
	days := int(end.Sub(start).Hours()/24) + 1
	if days <= 0 {
		return "Net amortized cost"
	}
	if days == 1 {
		return "Net amortized cost (1 day)"
	}
	return fmt.Sprintf("Net amortized cost (%d days)", days)
}

func accountSummaryLabel(r cost.CostResult) string {
	multi := strings.Contains(r.AccountName, ", ")
	if r.Linked {
		if multi {
			return "Accounts"
		}
		return "Account"
	}
	if multi {
		return "Payers"
	}
	return "Payer"
}

func formatAccountList(names, ids string) string {
	nameParts := strings.Split(names, ", ")
	idParts := strings.Split(ids, ", ")
	if len(nameParts) != len(idParts) {
		return fmt.Sprintf("%s (%s)", names, ids)
	}
	var parts []string
	for i := range nameParts {
		parts = append(parts, fmt.Sprintf("%s (%s)", strings.TrimSpace(nameParts[i]), strings.TrimSpace(idParts[i])))
	}
	return strings.Join(parts, ", ")
}

func cell(s styler, fn func(string) string, text string) string {
	if s.enabled {
		return fn(text)
	}
	return text
}

func breakdownTitles(splitBy cost.SplitBy) (title, keyHeader string) {
	switch splitBy {
	case cost.SplitByAccount:
		return "Cost by linked account", "ACCOUNT ID"
	case cost.SplitByOU:
		return "Cost by organizational unit (tree; amounts include nested OUs)", "OU"
	default:
		return "Cost by service", "SERVICE"
	}
}

func truncateLabel(name string) string {
	if len(name) <= maxServiceLen {
		return name
	}
	return name[:maxServiceLen-1] + "…"
}

func renderBar(s styler, fraction float64, rank int) string {
	if fraction < 0 {
		fraction = 0
	}
	if fraction > 1 {
		fraction = 1
	}
	filled := int(math.Round(fraction * float64(barWidth)))
	if filled > barWidth {
		filled = barWidth
	}
	empty := barWidth - filled

	var b strings.Builder
	fillChar := "█"
	emptyChar := "░"
	if s.enabled {
		b.WriteString(s.barColor(rank))
	}
	b.WriteString(strings.Repeat(fillChar, filled))
	if s.enabled {
		b.WriteString(ansiBarEmpty)
	}
	b.WriteString(strings.Repeat(emptyChar, empty))
	if s.enabled {
		b.WriteString(ansiReset)
	}
	return b.String()
}
