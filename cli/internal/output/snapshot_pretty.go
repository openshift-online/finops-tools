// snapshot_pretty.go renders snapshot discovery results as summary lines and a detail table.
package output

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/olekukonko/tablewriter"
	"github.com/openshift-online/finops-tools/cli/internal/format"
	"github.com/openshift-online/finops-tools/core/snapshot"
)

func writeSnapshotPretty(w io.Writer, r snapshot.Result) error {
	s := newStyler(w)
	if err := writeSnapshotSummary(w, s, r); err != nil {
		return err
	}
	if err := writeSnapshotRegionWarnings(w, s, r.Summary.SkippedRegions); err != nil {
		return err
	}
	if len(r.Records) == 0 {
		return nil
	}
	return writeSnapshotDetailTable(w, s, r.Summary, r.Records)
}

func writeSnapshotSummary(w io.Writer, s styler, r snapshot.Result) error {
	costCtx := newSnapshotCostContext(r.Summary)
	lines := buildSnapshotSummaryLines(r.Summary, costCtx)
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
		value := ln.value
		if ln.emphasize && s.enabled {
			value = s.bold(s.yellow(value))
		}
		if _, err := fmt.Fprintf(w, "  %-*s  %s\n", labelWidth, label+":", value); err != nil {
			return err
		}
	}

	if len(r.Summary.ByKind) > 0 {
		if _, err := fmt.Fprintln(w); err != nil {
			return err
		}
		title := "By type (listed snapshots)"
		if s.enabled {
			title = s.bold(s.cyan(title))
		}
		if _, err := fmt.Fprintln(w, title); err != nil {
			return err
		}
		for _, ks := range r.Summary.ByKind {
			kindCost := scaleSnapshotCost(ks.EstimatedMonthlyCostUSD, ks.Kind, costCtx)
			line := fmt.Sprintf("  %-24s  %4d    %s %s", ks.Kind, ks.Count, format.FormatMoney(kindCost, "USD"), snapshotKindCostSuffix(ks.Kind, costCtx))
			if _, err := fmt.Fprintln(w, line); err != nil {
				return err
			}
		}
	}

	if r.Summary.CostDisclaimer != "" {
		if _, err := fmt.Fprintln(w); err != nil {
			return err
		}
		note := r.Summary.CostDisclaimer
		if s.enabled {
			note = s.dim(note)
		}
		if _, err := fmt.Fprintf(w, "  %s\n", note); err != nil {
			return err
		}
	}
	return nil
}

func writeSnapshotRegionWarnings(w io.Writer, s styler, warnings []snapshot.RegionWarning) error {
	if len(warnings) == 0 {
		return nil
	}
	if _, err := fmt.Fprintln(w); err != nil {
		return err
	}
	title := "Skipped regions"
	if s.enabled {
		title = s.bold(s.cyan(title))
	}
	if _, err := fmt.Fprintln(w, title); err != nil {
		return err
	}

	table := newSnapshotTable(w)
	table.SetHeader([]string{
		cell(s, s.bold, "ACCOUNT"),
		cell(s, s.bold, "REGION"),
		cell(s, s.bold, "MESSAGE"),
	})
	for _, warning := range warnings {
		table.Append([]string{warning.AccountID, warning.Region, warning.Message})
	}
	table.Render()
	return nil
}

func writeSnapshotDetailTable(w io.Writer, s styler, summary snapshot.Summary, records []snapshot.Record) error {
	costCtx := newSnapshotCostContext(summary)
	if _, err := fmt.Fprintln(w); err != nil {
		return err
	}
	title := "Snapshots"
	if s.enabled {
		title = s.bold(s.cyan(title))
	}
	if _, err := fmt.Fprintln(w, title); err != nil {
		return err
	}

	table := newSnapshotTable(w)
	table.SetHeader([]string{
		cell(s, s.bold, "ACCOUNT"),
		cell(s, s.bold, "REGION"),
		cell(s, s.bold, "TYPE"),
		cell(s, s.bold, "SNAPSHOT ID"),
		cell(s, s.bold, "SOURCE"),
		cell(s, s.bold, "AGE"),
		cell(s, s.bold, "SIZE"),
		cell(s, s.bold, snapshotMonthlyCostColumnHeader(records, costCtx)),
		cell(s, s.bold, "CREATED"),
	})

	for _, rec := range records {
		created := rec.CreatedAt.UTC().Format(time.RFC3339)
		if len(created) > 19 {
			created = created[:19] + "Z"
		}
		table.Append([]string{
			rec.AccountID,
			rec.Region,
			string(rec.Kind),
			rec.ResourceID,
			snapshotSourceLabel(rec),
			fmt.Sprintf("%dd", rec.AgeDays),
			fmt.Sprintf("%.0fGiB", rec.SizeGiB),
			snapshotRecordMonthlyCost(rec, costCtx),
			created,
		})
	}
	table.Render()
	return nil
}

func newSnapshotTable(w io.Writer) *tablewriter.Table {
	table := tablewriter.NewWriter(w)
	table.SetAutoWrapText(false)
	table.SetBorder(false)
	table.SetAutoFormatHeaders(false)
	table.SetHeaderAlignment(tablewriter.ALIGN_LEFT)
	table.SetAlignment(tablewriter.ALIGN_LEFT)
	table.SetTablePadding("\t")
	return table
}

func snapshotSourceLabel(rec snapshot.Record) string {
	if rec.SourceResourceID != "" {
		return rec.SourceResourceID
	}
	return "-"
}

func snapshotBillingPeriodLabel(period snapshot.BilledSnapshotPeriod) string {
	start := strings.TrimSpace(period.StartDate)
	if start == "" {
		return "last complete month"
	}
	t, err := time.Parse("2006-01-02", start)
	if err != nil {
		return start
	}
	return t.Format("January 2006")
}

type snapshotSummaryLine struct {
	label     string
	value     string
	emphasize bool
}

type snapshotCostContext struct {
	billedEBS    float64
	billedRDS    float64
	billedPeriod string
	ebsCEScale   float64
	rdsCEScale   float64
}

func newSnapshotCostContext(summary snapshot.Summary) snapshotCostContext {
	ctx := snapshotCostContext{
		billedEBS: sumBilledEBSSnapshotUSD(summary.BilledCosts),
		billedRDS: sumBilledRDSBackupUSD(summary.BilledCosts),
	}
	if len(summary.BilledCosts) > 0 {
		ctx.billedPeriod = snapshotBillingPeriodLabel(summary.BilledCosts[0].Period)
	}
	if ctx.billedEBS > 0 && summary.EBSEstimatedMonthlyRunRateUSD > 0 {
		ctx.ebsCEScale = ctx.billedEBS / summary.EBSEstimatedMonthlyRunRateUSD
	}
	if ctx.billedRDS > 0 && summary.RDSBackupEstimatedMonthlyRunRateUSD > 0 {
		ctx.rdsCEScale = ctx.billedRDS / summary.RDSBackupEstimatedMonthlyRunRateUSD
	}
	return ctx
}

func buildSnapshotSummaryLines(summary snapshot.Summary, ctx snapshotCostContext) []snapshotSummaryLine {
	lines := []snapshotSummaryLine{{
		label: fmt.Sprintf("Snapshots (older than %d days)", summary.OlderThanDays),
		value: fmt.Sprintf("%d", summary.TotalCount),
	}}

	listedCost := listedSnapshotCostUSD(summary, ctx)
	period := ctx.billedPeriod
	if period == "" {
		period = "last complete month"
	}

	switch summaryListedCostBasis(summary, ctx) {
	case snapshotListedCostAttributed:
		if listedCost > 0 {
			lines = append(lines, snapshotSummaryLine{
				label:     fmt.Sprintf("Attributed cost (listed snapshots, %s)", period),
				value:     format.FormatMoney(listedCost, "USD"),
				emphasize: true,
			})
		}
	case snapshotListedCostMixed:
		if listedCost > 0 {
			lines = append(lines, snapshotSummaryLine{
				label:     fmt.Sprintf("Cost (listed snapshots, %s)", period),
				value:     format.FormatMoney(listedCost, "USD"),
				emphasize: true,
			})
		}
	case snapshotListedCostEstimated:
		if summary.EstimatedMonthlyCostUSD > 0 {
			lines = append(lines, snapshotSummaryLine{
				label:     "Estimated monthly cost (listed snapshots)",
				value:     format.FormatMoney(summary.EstimatedMonthlyCostUSD, "USD"),
				emphasize: true,
			})
		}
	}

	return lines
}

type snapshotListedCostBasis int

const (
	snapshotListedCostEstimated snapshotListedCostBasis = iota
	snapshotListedCostAttributed
	snapshotListedCostMixed
)

func snapshotKindUsesCEScale(kind snapshot.Kind, ctx snapshotCostContext) bool {
	switch kind {
	case snapshot.KindEBSSnapshot:
		return ctx.ebsCEScale > 0
	case snapshot.KindRDSSnapshot, snapshot.KindRDSClusterSnapshot:
		return ctx.rdsCEScale > 0
	default:
		return false
	}
}

func snapshotKindCostSuffix(kind snapshot.Kind, ctx snapshotCostContext) string {
	if snapshotKindUsesCEScale(kind, ctx) {
		return "attributed"
	}
	return "estimated"
}

func summaryListedCostBasis(summary snapshot.Summary, ctx snapshotCostContext) snapshotListedCostBasis {
	if len(summary.ByKind) > 0 {
		var hasAttributed, hasEstimated bool
		for _, ks := range summary.ByKind {
			if ks.EstimatedMonthlyCostUSD <= 0 {
				continue
			}
			if snapshotKindUsesCEScale(ks.Kind, ctx) {
				hasAttributed = true
			} else {
				hasEstimated = true
			}
		}
		switch {
		case hasAttributed && hasEstimated:
			return snapshotListedCostMixed
		case hasAttributed:
			return snapshotListedCostAttributed
		default:
			return snapshotListedCostEstimated
		}
	}
	if ctx.billedEBS > 0 && ctx.ebsCEScale > 0 && summary.RDSBackupEstimatedMonthlyRunRateUSD == 0 && summary.RDSBackupRegionalExcessGiB == 0 {
		return snapshotListedCostAttributed
	}
	return snapshotListedCostEstimated
}

func snapshotRecordCostBasis(records []snapshot.Record, ctx snapshotCostContext) snapshotListedCostBasis {
	var hasAttributed, hasEstimated bool
	for _, rec := range records {
		if rec.EstimatedMonthlyCostUSD <= 0 && rec.Kind != snapshot.KindEBSSnapshot {
			continue
		}
		if snapshotKindUsesCEScale(rec.Kind, ctx) {
			hasAttributed = true
		} else if rec.EstimatedMonthlyCostUSD > 0 {
			hasEstimated = true
		}
	}
	switch {
	case hasAttributed && hasEstimated:
		return snapshotListedCostMixed
	case hasAttributed:
		return snapshotListedCostAttributed
	default:
		return snapshotListedCostEstimated
	}
}

func listedSnapshotCostUSD(summary snapshot.Summary, ctx snapshotCostContext) float64 {
	if ctx.billedEBS > 0 || ctx.billedRDS > 0 {
		var total float64
		for _, ks := range summary.ByKind {
			total += scaleSnapshotCost(ks.EstimatedMonthlyCostUSD, ks.Kind, ctx)
		}
		if total > 0 {
			return total
		}
	}
	if ctx.billedEBS > 0 && ctx.ebsCEScale > 0 && summary.RDSBackupEstimatedMonthlyRunRateUSD == 0 && summary.RDSBackupRegionalExcessGiB == 0 {
		return summary.EstimatedMonthlyCostUSD * ctx.ebsCEScale
	}
	return summary.EstimatedMonthlyCostUSD
}

func sumBilledEBSSnapshotUSD(costs []snapshot.AccountBilledSnapshotCosts) float64 {
	var total float64
	for _, row := range costs {
		total += row.EBSSnapshotUSD
	}
	return total
}

func sumBilledRDSBackupUSD(costs []snapshot.AccountBilledSnapshotCosts) float64 {
	var total float64
	for _, row := range costs {
		total += row.RDSBackupUSD
	}
	return total
}

func snapshotMonthlyCostColumnHeader(records []snapshot.Record, ctx snapshotCostContext) string {
	switch snapshotRecordCostBasis(records, ctx) {
	case snapshotListedCostAttributed:
		return "$/MO"
	case snapshotListedCostMixed:
		return "COST/MO"
	default:
		return "EST/MO"
	}
}

func snapshotRecordMonthlyCost(rec snapshot.Record, ctx snapshotCostContext) string {
	cost := scaleSnapshotCost(rec.EstimatedMonthlyCostUSD, rec.Kind, ctx)
	if rec.Kind == snapshot.KindEBSSnapshot {
		if cost <= 0 {
			return "—"
		}
	}
	return format.FormatMoney(cost, "USD")
}

func scaleSnapshotCost(apiCost float64, kind snapshot.Kind, ctx snapshotCostContext) float64 {
	if kind == snapshot.KindEBSSnapshot {
		return scaleEBSCost(apiCost, kind, ctx)
	}
	if kind == snapshot.KindRDSSnapshot || kind == snapshot.KindRDSClusterSnapshot {
		return scaleRDSCost(apiCost, kind, ctx)
	}
	return apiCost
}

func scaleRDSCost(apiCost float64, kind snapshot.Kind, ctx snapshotCostContext) float64 {
	if (kind != snapshot.KindRDSSnapshot && kind != snapshot.KindRDSClusterSnapshot) || apiCost <= 0 || ctx.rdsCEScale <= 0 {
		return apiCost
	}
	return apiCost * ctx.rdsCEScale
}

func scaleEBSCost(apiCost float64, kind snapshot.Kind, ctx snapshotCostContext) float64 {
	if kind != snapshot.KindEBSSnapshot || apiCost <= 0 || ctx.ebsCEScale <= 0 {
		return apiCost
	}
	return apiCost * ctx.ebsCEScale
}
