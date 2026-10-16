// extendedsupport.go formats core/extendedsupport results (pretty-print, JSON, CSV).
package output

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"

	"github.com/openshift-online/finops-tools/core/extendedsupport"
)

// WriteExtendedSupportListResult writes extended-support discovery results in the requested format.
func WriteExtendedSupportListResult(w io.Writer, format Format, r extendedsupport.Result) error {
	switch format {
	case FormatPrettyPrint:
		return writeExtendedSupportPretty(w, r)
	case FormatJSON:
		return writeExtendedSupportJSON(w, r)
	case FormatCSV:
		return writeExtendedSupportCSV(w, r)
	default:
		return fmt.Errorf("unknown format %q", format)
	}
}

func writeExtendedSupportJSON(w io.Writer, r extendedsupport.Result) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(r)
}

func writeExtendedSupportCSV(w io.Writer, r extendedsupport.Result) error {
	cw := csv.NewWriter(w)

	header := []string{
		"account_id", "region", "service", "resource_type", "resource_id",
		"engine", "engine_version", "major_engine_version",
		"standard_support_end_date", "extended_support_end_date", "engine_lifecycle_support",
	}
	if err := cw.Write(header); err != nil {
		return err
	}
	for _, rec := range r.Records {
		extendedEnd := ""
		if rec.ExtendedSupportEndDate != nil {
			extendedEnd = rec.ExtendedSupportEndDate.Format("2006-01-02")
		}
		row := []string{
			sanitizeCSVField(rec.AccountID),
			sanitizeCSVField(rec.Region),
			string(rec.Service),
			string(rec.ResourceType),
			sanitizeCSVField(rec.ResourceID),
			sanitizeCSVField(rec.Engine),
			sanitizeCSVField(rec.EngineVersion),
			sanitizeCSVField(rec.MajorEngineVersion),
			rec.StandardSupportEndDate.Format("2006-01-02"),
			extendedEnd,
			sanitizeCSVField(rec.EngineLifecycleSupport),
		}
		if err := cw.Write(row); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}
