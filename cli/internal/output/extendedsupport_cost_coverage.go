package output

import (
	"fmt"
	"io"

	"github.com/openshift-online/finops-tools/core/extendedsupport"
)

func writeExtendedSupportCostCoverage(w io.Writer, s styler, coverage *extendedsupport.CostCoverage) error {
	if coverage == nil {
		return nil
	}
	unit, column := "Months", "MONTH"
	if coverage.Granularity == "daily" {
		unit, column = "Days", "DAY"
	}
	if _, err := fmt.Fprintf(w, "\nPeriod coverage\n  %s with matching records: %d of %d\n  %s\n",
		unit, coverage.PeriodsWithRecords, coverage.TotalPeriods, coverage.Notice); err != nil {
		return err
	}
	var rows [][]string
	for _, period := range coverage.Periods {
		if period.Status == extendedsupport.CostPeriodMatchingCharges && !period.Incomplete {
			continue
		}
		label := period.StartDate
		if coverage.Granularity == "monthly" && len(label) >= 7 {
			label = label[:7]
		}
		status := string(period.Status)
		switch period.Status {
		case extendedsupport.CostPeriodMatchingCharges:
			status = "Matching charge records returned"
		case extendedsupport.CostPeriodNoMatchingCharges:
			status = "No matching charges returned"
		case extendedsupport.CostPeriodNotReturned:
			status = "No period returned"
		case extendedsupport.CostPeriodNotQueried:
			status = "No matching usage types; costs not queried"
		}
		if period.Incomplete {
			status += "; some period responses missing"
		}
		rows = append(rows, []string{label, status})
	}
	if len(rows) > 0 {
		table := newExtendedSupportTable(w)
		table.SetHeader([]string{cell(s, s.bold, column), cell(s, s.bold, "STATUS")})
		table.AppendBulk(rows)
		table.Render()
	}
	return nil
}
