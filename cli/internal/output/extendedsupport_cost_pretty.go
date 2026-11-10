package output

import (
	"fmt"
	"io"
	"strings"

	"github.com/openshift-online/finops-tools/cli/internal/format"
	"github.com/openshift-online/finops-tools/core/extendedsupport"
)

func writeExtendedSupportCostPretty(w io.Writer, r extendedsupport.CostResult) error {
	s := newStyler(w)
	if len(r.Regions) > 0 {
		if _, err := fmt.Fprintf(w, "  Requested billing regions: %s\n", SanitizeTerminal(strings.Join(r.Regions, ", "))); err != nil {
			return err
		}
	}
	if len(r.ExcludedRegions) > 0 {
		if _, err := fmt.Fprintf(w, "  Excluded billing regions: %s\n", SanitizeTerminal(strings.Join(r.ExcludedRegions, ", "))); err != nil {
			return err
		}
	}
	services := make([]string, len(r.Services))
	for i, service := range r.Services {
		services[i] = string(service)
	}
	if _, err := fmt.Fprintf(w, "  Period: %s – %s (inclusive, UTC)\n  Services: %s\n  Granularity: %s\n  Group by: %s\n  Metric: %s\n  Recorded Extended Support charges: %s\n  Identified avoidable surcharges: %s\n",
		r.StartDate, r.EndDate, strings.Join(services, ", "), r.Granularity, r.GroupBy, r.Metric, supportCostMoney(r.Amount, r.Currency), supportAvoidableText(r.AvoidableAmount, r.Currency)); err != nil {
		return err
	}
	if r.AvoidableAmount == nil {
		if _, err := fmt.Fprintf(w, "  Known avoidable subtotal (incomplete): %s\n", supportCostMoney(r.KnownAvoidableAmount, r.Currency)); err != nil {
			return err
		}
	}
	if r.Estimated {
		if _, err := fmt.Fprintln(w, "  Cost Explorer marks part of this period as estimated."); err != nil {
			return err
		}
	}
	for _, notice := range r.Notices {
		if _, err := fmt.Fprintf(w, "\n  %s\n", notice); err != nil {
			return err
		}
	}
	if err := writeExtendedSupportCostCoverage(w, s, r.Coverage); err != nil {
		return err
	}
	if len(r.Rows) == 0 {
		_, err := fmt.Fprintln(w, "\nNo matching Extended Support charges were returned for the selected services and period.")
		return err
	}
	if _, err := fmt.Fprintln(w); err != nil {
		return err
	}
	if r.Granularity != "total" && len(r.ServiceTotals) > 0 {
		if _, err := fmt.Fprintln(w, "Totals by service for full period"); err != nil {
			return err
		}
		totals := newExtendedSupportTable(w)
		totals.SetHeader([]string{cell(s, s.bold, "SERVICE"), cell(s, s.bold, "RECORDED CHARGES")})
		for _, total := range r.ServiceTotals {
			totals.Append([]string{string(total.Service), supportCostMoney(total.Amount, r.Currency)})
		}
		totals.Render()
		if _, err := fmt.Fprintln(w, "\nTime breakdown"); err != nil {
			return err
		}
	}
	table := newExtendedSupportTable(w)
	var headers []string
	if r.Granularity != "total" {
		headers = append(headers, cell(s, s.bold, "FROM"), cell(s, s.bold, "TO"))
	}
	if r.GroupBy != extendedsupport.CostGroupByAccount {
		headers = append(headers, cell(s, s.bold, "SERVICE"))
	}
	if r.GroupBy != extendedsupport.CostGroupByService {
		headers = append(headers, cell(s, s.bold, "ACCOUNT"))
	}
	headers = append(headers, cell(s, s.bold, "RECORDED CHARGES"))
	table.SetHeader(headers)
	for _, row := range r.Rows {
		var cells []string
		if r.Granularity != "total" {
			cells = append(cells, row.StartDate, row.EndDate)
		}
		if r.GroupBy != extendedsupport.CostGroupByAccount {
			cells = append(cells, string(row.Service))
		}
		if r.GroupBy != extendedsupport.CostGroupByService {
			cells = append(cells, row.AccountID)
		}
		cells = append(cells, supportCostMoney(row.Amount, r.Currency))
		table.Append(cells)
	}
	table.Render()
	return nil
}

func supportAvoidableText(amount *float64, currency string) string {
	if amount == nil {
		return "unavailable"
	}
	return supportCostMoney(*amount, currency)
}

func supportCostMoney(amount float64, currency string) string {
	if currency == "" {
		return format.FormatAmount(amount) + " (currency unavailable)"
	}
	return format.FormatMoney(amount, currency)
}
