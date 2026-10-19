// extendedsupport_pretty.go renders extended-support results as summary lines and a detail table.
package output

import (
	"fmt"
	"io"

	"github.com/olekukonko/tablewriter"
	"github.com/openshift-online/finops-tools/core/extendedsupport"
)

func writeExtendedSupportPretty(w io.Writer, r extendedsupport.Result) error {
	s := newStyler(w)
	if err := writeExtendedSupportSummary(w, s, r); err != nil {
		return err
	}
	if err := writeExtendedSupportRegionWarnings(w, s, r.Summary.SkippedRegions); err != nil {
		return err
	}
	if err := writeExtendedSupportAccountWarnings(w, s, r.Summary.SkippedAccounts); err != nil {
		return err
	}
	if err := writeExtendedSupportResourceWarnings(w, s, r.Summary.UnclassifiedResources); err != nil {
		return err
	}
	if len(r.Records) == 0 {
		return nil
	}
	return writeExtendedSupportDetailTable(w, s, r.Records)
}

func writeExtendedSupportSummary(w io.Writer, s styler, r extendedsupport.Result) error {
	label := "Resources in extended support:"
	value := fmt.Sprintf("%d", r.Summary.TotalCount)
	if s.enabled {
		label = s.dim(label)
		value = s.bold(s.yellow(value))
	}
	if _, err := fmt.Fprintf(w, "  %-*s  %s\n", len("Resources in extended support:"), label, value); err != nil {
		return err
	}

	if len(r.Summary.ByService) > 0 {
		if _, err := fmt.Fprintln(w); err != nil {
			return err
		}
		title := "By service"
		if s.enabled {
			title = s.bold(s.cyan(title))
		}
		if _, err := fmt.Fprintln(w, title); err != nil {
			return err
		}
		for _, summary := range r.Summary.ByService {
			if _, err := fmt.Fprintf(w, "  %-24s  %4d\n", summary.Service, summary.Count); err != nil {
				return err
			}
		}
	}

	if len(r.Summary.ByAccount) > 0 {
		if _, err := fmt.Fprintln(w); err != nil {
			return err
		}
		title := "By account"
		if s.enabled {
			title = s.bold(s.cyan(title))
		}
		if _, err := fmt.Fprintln(w, title); err != nil {
			return err
		}
		for _, summary := range r.Summary.ByAccount {
			if _, err := fmt.Fprintf(w, "  %-24s  %4d\n", summary.AccountID, summary.Count); err != nil {
				return err
			}
		}
	}
	return nil
}

func writeExtendedSupportRegionWarnings(w io.Writer, s styler, warnings []extendedsupport.RegionWarning) error {
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

	table := newExtendedSupportTable(w)
	table.SetHeader([]string{
		cell(s, s.bold, "ACCOUNT"),
		cell(s, s.bold, "REGION"),
		cell(s, s.bold, "SERVICE"),
		cell(s, s.bold, "MESSAGE"),
	})
	for _, warning := range warnings {
		table.Append([]string{warning.AccountID, warning.Region, string(warning.Service), warning.Message})
	}
	table.Render()
	return nil
}

func writeExtendedSupportAccountWarnings(w io.Writer, s styler, warnings []extendedsupport.AccountWarning) error {
	if len(warnings) == 0 {
		return nil
	}
	if _, err := fmt.Fprintln(w); err != nil {
		return err
	}
	title := "Skipped accounts"
	if s.enabled {
		title = s.bold(s.cyan(title))
	}
	if _, err := fmt.Fprintln(w, title); err != nil {
		return err
	}

	table := newExtendedSupportTable(w)
	table.SetHeader([]string{
		cell(s, s.bold, "ACCOUNT"),
		cell(s, s.bold, "MESSAGE"),
	})
	for _, warning := range warnings {
		table.Append([]string{warning.AccountID, warning.Message})
	}
	table.Render()
	return nil
}

func writeExtendedSupportResourceWarnings(w io.Writer, s styler, warnings []extendedsupport.ResourceWarning) error {
	if len(warnings) == 0 {
		return nil
	}
	if _, err := fmt.Fprintln(w); err != nil {
		return err
	}
	title := "Support status unknown"
	if s.enabled {
		title = s.bold(s.cyan(title))
	}
	if _, err := fmt.Fprintln(w, title); err != nil {
		return err
	}
	table := newExtendedSupportTable(w)
	table.SetHeader([]string{
		cell(s, s.bold, "ACCOUNT"),
		cell(s, s.bold, "REGION"),
		cell(s, s.bold, "SERVICE"),
		cell(s, s.bold, "TYPE"),
		cell(s, s.bold, "RESOURCE ID"),
		cell(s, s.bold, "ENGINE"),
		cell(s, s.bold, "VERSION"),
		cell(s, s.bold, "MESSAGE"),
	})
	for _, warning := range warnings {
		table.Append([]string{warning.AccountID, warning.Region, string(warning.Service), string(warning.ResourceType), warning.ResourceID, warning.Engine, warning.EngineVersion, warning.Message})
	}
	table.Render()
	return nil
}

func writeExtendedSupportDetailTable(w io.Writer, s styler, records []extendedsupport.Record) error {
	if _, err := fmt.Fprintln(w); err != nil {
		return err
	}
	title := "Resources in extended support"
	if s.enabled {
		title = s.bold(s.cyan(title))
	}
	if _, err := fmt.Fprintln(w, title); err != nil {
		return err
	}

	table := newExtendedSupportTable(w)
	table.SetHeader([]string{
		cell(s, s.bold, "ACCOUNT"),
		cell(s, s.bold, "REGION"),
		cell(s, s.bold, "SERVICE"),
		cell(s, s.bold, "TYPE"),
		cell(s, s.bold, "RESOURCE ID"),
		cell(s, s.bold, "ENGINE"),
		cell(s, s.bold, "VERSION"),
		cell(s, s.bold, "STD SUPPORT ENDED"),
		cell(s, s.bold, "EXT SUPPORT ENDS"),
	})
	for _, rec := range records {
		extendedEnd := "—"
		if rec.ExtendedSupportEndDate != nil {
			extendedEnd = rec.ExtendedSupportEndDate.Format("2006-01-02")
		}
		table.Append([]string{
			rec.AccountID,
			rec.Region,
			string(rec.Service),
			string(rec.ResourceType),
			rec.ResourceID,
			rec.Engine,
			rec.EngineVersion,
			rec.StandardSupportEndDate.Format("2006-01-02"),
			extendedEnd,
		})
	}
	table.Render()
	return nil
}

func newExtendedSupportTable(w io.Writer) *tablewriter.Table {
	table := tablewriter.NewWriter(w)
	table.SetAutoWrapText(false)
	table.SetBorder(false)
	table.SetAutoFormatHeaders(false)
	table.SetHeaderAlignment(tablewriter.ALIGN_LEFT)
	table.SetAlignment(tablewriter.ALIGN_LEFT)
	table.SetTablePadding("\t")
	return table
}
