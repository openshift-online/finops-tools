// snapshot_pretty.go renders snapshot discovery results as summary lines and a detail table.
package output

import (
	"fmt"
	"io"
	"time"

	"github.com/olekukonko/tablewriter"
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
	return writeSnapshotDetailTable(w, s, r.Records)
}

func writeSnapshotSummary(w io.Writer, s styler, r snapshot.Result) error {
	lines := []struct{ label, value string }{
		{
			fmt.Sprintf("Snapshots (older than %d days)", r.Summary.OlderThanDays),
			fmt.Sprintf("%d", r.Summary.TotalCount),
		},
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

	if len(r.Summary.ByKind) > 0 {
		if _, err := fmt.Fprintln(w); err != nil {
			return err
		}
		title := "By type"
		if s.enabled {
			title = s.bold(s.cyan(title))
		}
		if _, err := fmt.Fprintln(w, title); err != nil {
			return err
		}
		for _, ks := range r.Summary.ByKind {
			line := fmt.Sprintf("  %-24s  %4d", ks.Kind, ks.Count)
			if _, err := fmt.Fprintln(w, line); err != nil {
				return err
			}
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

func writeSnapshotDetailTable(w io.Writer, s styler, records []snapshot.Record) error {
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
