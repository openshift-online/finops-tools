package output

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/openshift-online/finops-tools/core/extendedsupport"
)

func TestExtendedSupportCostCoverageAppearsBeforeTotalsAndBreakdown(t *testing.T) {
	for _, granularity := range []string{"monthly", "daily", "total"} {
		t.Run(granularity, func(t *testing.T) {
			r := supportCostFixture()
			r.Granularity, r.Amount = granularity, 50
			r.StartDate, r.EndDate = "2025-10-01", "2026-01-31"
			periods := []extendedsupport.CostPeriodCoverage{
				{StartDate: "2025-10-01", EndDate: "2025-10-31", Status: extendedsupport.CostPeriodNotReturned},
				{StartDate: "2025-11-01", EndDate: "2025-11-30", Status: extendedsupport.CostPeriodNoMatchingCharges},
				{StartDate: "2025-12-01", EndDate: "2025-12-31", Status: extendedsupport.CostPeriodMatchingCharges, Incomplete: true},
				{StartDate: "2026-01-01", EndDate: "2026-01-31", Status: extendedsupport.CostPeriodMatchingCharges},
			}
			coverageGranularity, wantCount, wantPeriod := "monthly", "Months with matching records: 2 of 4", "2025-10"
			if granularity == "daily" {
				coverageGranularity, wantCount, wantPeriod = "daily", "Days with matching records: 2 of 4", "2026-01-01"
				r.StartDate, r.EndDate = "2026-01-01", "2026-01-04"
				for i, day := range []string{"2026-01-01", "2026-01-02", "2026-01-03", "2026-01-04"} {
					periods[i].StartDate, periods[i].EndDate = day, day
				}
			}
			r.Coverage = &extendedsupport.CostCoverage{
				Granularity: coverageGranularity, TotalPeriods: 4, PeriodsWithRecords: 2, Periods: periods,
				Notice: "Periods without matching records may have no charges or unavailable history. Totals include only returned billing records.",
			}
			r.Rows = []extendedsupport.CostRow{
				{StartDate: periods[2].StartDate, EndDate: periods[2].EndDate, Service: extendedsupport.ServiceRDS, CostAmounts: extendedsupport.CostAmounts{Amount: 50}},
				{StartDate: periods[3].StartDate, EndDate: periods[3].EndDate, Service: extendedsupport.ServiceRDS, CostAmounts: extendedsupport.CostAmounts{Amount: 0}},
			}
			r.ServiceTotals = []extendedsupport.CostRow{
				{StartDate: r.StartDate, EndDate: r.EndDate, Service: extendedsupport.ServiceRDS, CostAmounts: extendedsupport.CostAmounts{Amount: 50}},
			}
			if granularity == "total" {
				r.Rows = r.ServiceTotals
				r.ServiceTotals = nil
			}
			var pretty bytes.Buffer
			if err := WriteExtendedSupportCostResult(&pretty, FormatPrettyPrint, r); err != nil {
				t.Fatal(err)
			}
			for _, want := range []string{"Period coverage", wantCount, wantPeriod, "No matching charges returned", "No period returned", "Matching charge records returned; some period responses missing", "unavailable history", "Totals include only returned billing records"} {
				if !strings.Contains(pretty.String(), want) {
					t.Fatalf("missing %q: %s", want, &pretty)
				}
			}
			sections := strings.Split(pretty.String(), "Period coverage\n")
			if len(sections) != 2 || !strings.Contains(sections[1], "RECORDED CHARGES") {
				t.Fatalf("coverage is not above the cost tables: %s", &pretty)
			}
			if granularity != "total" && (!strings.Contains(sections[1], "Time breakdown") || !strings.Contains(sections[1], "USD 0.00")) {
				t.Fatalf("breakdown or explicit zero lost: %s", &pretty)
			}
			var encoded bytes.Buffer
			if err := WriteExtendedSupportCostResult(&encoded, FormatJSON, r); err != nil {
				t.Fatal(err)
			}
			var decoded extendedsupport.CostResult
			if err := json.Unmarshal(encoded.Bytes(), &decoded); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(decoded.Coverage, r.Coverage) {
				t.Fatalf("JSON lost period coverage: %s", &encoded)
			}
			var csvOutput bytes.Buffer
			if err := WriteExtendedSupportCostResult(&csvOutput, FormatCSV, r); err != nil {
				t.Fatal(err)
			}
			rows, err := csv.NewReader(&csvOutput).ReadAll()
			if err != nil || len(rows) != len(r.Rows)+1 || strings.Contains(csvOutput.String(), "No period returned") {
				t.Fatalf("coverage leaked into CSV cost rows: %s, err=%v", &csvOutput, err)
			}
		})
	}
}

func TestExtendedSupportCostCoverageRemainsVisibleWithoutCostRows(t *testing.T) {
	r := extendedsupport.CostResult{
		StartDate: "2026-01-01", EndDate: "2026-01-31", Granularity: "total",
		Coverage: &extendedsupport.CostCoverage{
			Granularity: "monthly", TotalPeriods: 1,
			Periods: []extendedsupport.CostPeriodCoverage{{StartDate: "2026-01-01", EndDate: "2026-01-31", Status: extendedsupport.CostPeriodNotQueried}},
		},
	}
	var pretty bytes.Buffer
	if err := WriteExtendedSupportCostResult(&pretty, FormatPrettyPrint, r); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Period coverage", "Months with matching records: 0 of 1", "2026-01", "No matching usage types; costs not queried", "No matching Extended Support charges were returned"} {
		if !strings.Contains(pretty.String(), want) {
			t.Fatalf("missing %q: %s", want, &pretty)
		}
	}
	if strings.Contains(pretty.String(), "Time breakdown") || strings.Contains(pretty.String(), "Totals by service for full period") {
		t.Fatalf("empty report added cost tables: %s", &pretty)
	}
}
