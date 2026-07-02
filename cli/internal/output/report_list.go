package output

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	reportpkg "github.com/openshift-online/finops-tools/cli/internal/report"
	"github.com/olekukonko/tablewriter"
)

// WriteReportTemplateList prints available report templates in the requested format.
func WriteReportTemplateList(w io.Writer, format Format, templates []reportpkg.TemplateInfo) error {
	switch format {
	case FormatPrettyPrint:
		return writeReportTemplateListPretty(w, templates)
	case FormatJSON:
		return writeReportTemplateListJSON(w, templates)
	case FormatCSV:
		return writeReportTemplateListCSV(w, templates)
	default:
		return fmt.Errorf("unsupported format %q", format)
	}
}

func writeReportTemplateListPretty(w io.Writer, templates []reportpkg.TemplateInfo) error {
	s := newStyler(w)
	if len(templates) == 0 {
		msg := "No report templates available."
		if s.enabled {
			msg = s.dim(msg)
		}
		_, err := fmt.Fprintln(w, msg)
		return err
	}

	title := "Available report templates:"
	if s.enabled {
		title = s.bold(s.cyan(title))
	}
	if _, err := fmt.Fprintln(w, title); err != nil {
		return err
	}

	table := tablewriter.NewWriter(w)
	table.SetAutoWrapText(false)
	table.SetBorder(false)
	table.SetHeaderAlignment(tablewriter.ALIGN_LEFT)
	table.SetAlignment(tablewriter.ALIGN_LEFT)
	table.SetTablePadding("\t")
	table.SetHeader([]string{
		cell(s, s.bold, "TEMPLATE"),
		cell(s, s.bold, "FORMATS"),
		cell(s, s.bold, "DESCRIPTION"),
	})

	for _, t := range templates {
		formats := strings.Join(t.Formats, ", ")
		if s.enabled {
			formats = s.dim(formats)
		}
		table.Append([]string{
			cell(s, s.bold, t.Name),
			formats,
			t.Description,
		})
	}
	table.Render()
	return nil
}

type reportTemplateJSONRow struct {
	Name        string   `json:"name"`
	Formats     []string `json:"formats"`
	Description string   `json:"description"`
}

func writeReportTemplateListJSON(w io.Writer, templates []reportpkg.TemplateInfo) error {
	rows := make([]reportTemplateJSONRow, len(templates))
	for i, t := range templates {
		rows[i] = reportTemplateJSONRow{
			Name:        t.Name,
			Formats:     append([]string(nil), t.Formats...),
			Description: t.Description,
		}
	}
	payload := struct {
		Templates []reportTemplateJSONRow `json:"templates"`
	}{Templates: rows}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(payload)
}

func writeReportTemplateListCSV(w io.Writer, templates []reportpkg.TemplateInfo) error {
	cw := csv.NewWriter(w)
	if err := cw.Write([]string{"template", "formats", "description"}); err != nil {
		return err
	}
	for _, t := range templates {
		if err := cw.Write([]string{
			t.Name,
			strings.Join(t.Formats, ","),
			t.Description,
		}); err != nil {
			return err
		}
	}
	cw.Flush()
	if err := cw.Error(); err != nil {
		return fmt.Errorf("write csv: %w", err)
	}
	return nil
}
