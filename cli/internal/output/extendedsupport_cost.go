package output

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/openshift-online/finops-tools/core/extendedsupport"
)

func WriteExtendedSupportCostResult(w io.Writer, format Format, result extendedsupport.CostResult) error {
	switch format {
	case FormatJSON:
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(result)
	case FormatCSV:
		return writeExtendedSupportCostCSV(w, result)
	case FormatPrettyPrint:
		return writeExtendedSupportCostPretty(w, result)
	default:
		return fmt.Errorf("unknown format %q", format)
	}
}

func writeExtendedSupportCostCSV(w io.Writer, r extendedsupport.CostResult) error {
	cw := csv.NewWriter(w)
	if err := cw.Write([]string{"start_date", "end_date", "service", "account_id", "metric", "currency", "amount", "avoidable_amount", "known_avoidable_amount", "estimated", "notices", "regions", "excluded_regions"}); err != nil {
		return err
	}
	for _, row := range r.Rows {
		avoidable := ""
		if row.AvoidableAmount != nil {
			avoidable = strconv.FormatFloat(*row.AvoidableAmount, 'f', -1, 64)
		}
		if err := cw.Write([]string{
			row.StartDate, row.EndDate, string(row.Service), sanitizeCSVField(row.AccountID), r.Metric, sanitizeCSVField(r.Currency),
			strconv.FormatFloat(row.Amount, 'f', -1, 64), avoidable, strconv.FormatFloat(row.KnownAvoidableAmount, 'f', -1, 64),
			strconv.FormatBool(row.Estimated), sanitizeCSVField(strings.Join(r.Notices, " ")),
			sanitizeCSVField(strings.Join(r.Regions, ",")), sanitizeCSVField(strings.Join(r.ExcludedRegions, ",")),
		}); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}
