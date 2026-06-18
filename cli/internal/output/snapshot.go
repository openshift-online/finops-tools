// snapshot.go formats core/snapshot results for the terminal (pretty-print, JSON, CSV).
package output

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"strconv"

	"github.com/openshift-online/finops-tools/core/snapshot"
)

// WriteSnapshotListResult writes snapshot discovery results in the requested format.
func WriteSnapshotListResult(w io.Writer, format Format, r snapshot.Result) error {
	switch format {
	case FormatPrettyPrint:
		return writeSnapshotPretty(w, r)
	case FormatJSON:
		return writeSnapshotJSON(w, r)
	case FormatCSV:
		return writeSnapshotCSV(w, r)
	default:
		return fmt.Errorf("unknown format %q", format)
	}
}

func writeSnapshotJSON(w io.Writer, r snapshot.Result) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(r)
}

func writeSnapshotCSV(w io.Writer, r snapshot.Result) error {
	cw := csv.NewWriter(w)

	header := []string{
		"account_id", "region", "kind", "resource_id", "source_resource_id",
		"created_at", "age_days", "size_gib", "storage_tier", "snapshot_type",
		"estimated_monthly_cost_usd", "cost_basis", "description",
	}
	if err := cw.Write(header); err != nil {
		return err
	}
	for _, rec := range r.Records {
		row := []string{
			sanitizeCSVField(rec.AccountID),
			sanitizeCSVField(rec.Region),
			string(rec.Kind),
			sanitizeCSVField(rec.ResourceID),
			sanitizeCSVField(rec.SourceResourceID),
			rec.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
			strconv.Itoa(rec.AgeDays),
			strconv.FormatFloat(rec.SizeGiB, 'f', -1, 64),
			sanitizeCSVField(rec.StorageTier),
			sanitizeCSVField(rec.SnapshotType),
			strconv.FormatFloat(rec.EstimatedMonthlyCostUSD, 'f', -1, 64),
			sanitizeCSVField(rec.CostBasis),
			sanitizeCSVField(rec.Description),
		}
		if err := cw.Write(row); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}
