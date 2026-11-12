package output

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/openshift-online/finops-tools/core/extendedsupport"
)

func supportCostFixture() extendedsupport.CostResult {
	avoidable := 1234.567
	r := extendedsupport.CostResult{
		StartDate: "2026-10-01", EndDate: "2026-10-31", Granularity: "monthly", GroupBy: extendedsupport.CostGroupByService,
		Metric: "NetUnblendedCost", Currency: "USD", Estimated: true,
		Services: []extendedsupport.Service{extendedsupport.ServiceRDS, extendedsupport.ServiceEKS},
		Regions:  []string{"us-east-1", "us-west-2"}, ExcludedRegions: []string{"us-west-2"},
		CostAmounts: extendedsupport.CostAmounts{Amount: 1240.567, KnownAvoidableAmount: avoidable},
		Notices:     []string{"Avoidable amounts identify historical surcharges, not forecast savings. EKS baseline unavailable."},
		Rows: []extendedsupport.CostRow{
			{StartDate: "2026-10-01", EndDate: "2026-10-31", Service: extendedsupport.ServiceRDS,
				CostAmounts: extendedsupport.CostAmounts{Amount: avoidable, AvoidableAmount: &avoidable, KnownAvoidableAmount: avoidable}},
			{StartDate: "2026-10-01", EndDate: "2026-10-31", Service: extendedsupport.ServiceEKS, Estimated: true,
				CostAmounts: extendedsupport.CostAmounts{Amount: 6}},
		},
	}
	r.ServiceTotals = append([]extendedsupport.CostRow(nil), r.Rows...)
	return r
}

func TestExtendedSupportCostOutputPreservesUnknownAvoidableAmounts(t *testing.T) {
	r := supportCostFixture()
	for _, format := range []Format{FormatJSON, FormatCSV, FormatPrettyPrint} {
		t.Run(string(format), func(t *testing.T) {
			var buf bytes.Buffer
			if err := WriteExtendedSupportCostResult(&buf, format, r); err != nil {
				t.Fatal(err)
			}
			if strings.Contains(buf.String(), "\x1b[") {
				t.Fatal("ANSI in redirected output")
			}
			switch format {
			case FormatJSON:
				var decoded extendedsupport.CostResult
				if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
					t.Fatal(err)
				}
				if decoded.AvoidableAmount != nil || decoded.Rows[1].AvoidableAmount != nil || decoded.Rows[0].AvoidableAmount == nil || *decoded.Rows[0].AvoidableAmount != 1234.567 || decoded.KnownAvoidableAmount != 1234.567 || !decoded.Estimated {
					t.Fatalf("JSON=%s", buf.String())
				}
				if len(decoded.ServiceTotals) != 2 || decoded.ServiceTotals[0].Amount != 1234.567 || decoded.ServiceTotals[0].AvoidableAmount == nil || decoded.ServiceTotals[1].AvoidableAmount != nil || !decoded.ServiceTotals[1].Estimated {
					t.Fatalf("JSON service totals=%s", buf.String())
				}
				if strings.Join(decoded.Regions, ",") != "us-east-1,us-west-2" || strings.Join(decoded.ExcludedRegions, ",") != "us-west-2" {
					t.Fatalf("JSON region scope=%s", buf.String())
				}
			case FormatCSV:
				rows, err := csv.NewReader(&buf).ReadAll()
				if err != nil || len(rows) != 3 {
					t.Fatalf("rows=%v err=%v", rows, err)
				}
				if rows[1][6] != "1234.567" || rows[1][7] != "1234.567" || rows[2][7] != "" || rows[2][8] != "0" || rows[2][9] != "true" || !strings.Contains(rows[2][10], "EKS baseline unavailable") {
					t.Fatalf("CSV=%v", rows)
				}
				if rows[0][11] != "regions" || rows[0][12] != "excluded_regions" || rows[1][11] != "us-east-1,us-west-2" || rows[1][12] != "us-west-2" {
					t.Fatalf("CSV region scope=%v", rows)
				}
			case FormatPrettyPrint:
				for _, value := range []string{"USD 1,240.57", "USD 1,234.57", "unavailable", "incomplete", "NetUnblendedCost", "estimated", "rds", "eks", "EKS baseline unavailable", "Requested billing regions: us-east-1, us-west-2", "Excluded billing regions: us-west-2"} {
					if !strings.Contains(buf.String(), value) {
						t.Fatalf("missing %q: %s", value, buf.String())
					}
				}
			}
		})
	}
}

func TestExtendedSupportCostOutputEmptyAndErrors(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteExtendedSupportCostResult(&buf, FormatPrettyPrint, extendedsupport.CostResult{}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "No matching Extended Support charges") {
		t.Fatal(buf.String())
	}
	for _, format := range []Format{FormatJSON, FormatCSV, FormatPrettyPrint, Format("invalid")} {
		if err := WriteExtendedSupportCostResult(supportCostFailWriter{}, format, supportCostFixture()); err == nil {
			t.Fatalf("writer/format failure ignored: %s", format)
		}
	}
}

func TestExtendedSupportCostTotalsTableShowsGroupsWithoutRepeatedDates(t *testing.T) {
	r := supportCostFixture()
	r.Granularity = "total"
	r.Notices = append(r.Notices, "Defaulting to totals for the last 90 reliable Cost Explorer days.")
	var buf bytes.Buffer
	if err := WriteExtendedSupportCostResult(&buf, FormatPrettyPrint, r); err != nil {
		t.Fatal(err)
	}
	text := buf.String()
	for _, want := range []string{"SERVICE", "RECORDED CHARGES", "rds", "eks", "USD 1,234.57", "last 90 reliable Cost Explorer days"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q: %s", want, text)
		}
	}
	if strings.Contains(text, "Totals by service for full period") || strings.Contains(text, "Time breakdown") || strings.Contains(text, "FROM") || strings.Contains(text, "TO ") || strings.Contains(text, "ESTIMATED") || strings.Contains(text, "AVOIDABLE SURCHARGES") || strings.Count(text, "2026-10-01") != 1 {
		t.Fatalf("unexpected totals table columns: %s", text)
	}
}

func TestExtendedSupportCostBreakdownShowsFullPeriodServiceTotals(t *testing.T) {
	for _, granularity := range []string{"daily", "monthly"} {
		for _, grouping := range []extendedsupport.CostGroupBy{extendedsupport.CostGroupByService, extendedsupport.CostGroupByAccount, extendedsupport.CostGroupByServiceAccount} {
			t.Run(granularity+"/"+string(grouping), func(t *testing.T) {
				r := supportCostFixture()
				r.Granularity, r.GroupBy = granularity, grouping
				r.EndDate = "2026-11-01"
				r.Amount, r.KnownAvoidableAmount = 2512, 2500
				r.ServiceTotals = []extendedsupport.CostRow{
					{StartDate: r.StartDate, EndDate: r.EndDate, Service: extendedsupport.ServiceEKS, CostAmounts: extendedsupport.CostAmounts{Amount: 12}},
					{StartDate: r.StartDate, EndDate: r.EndDate, Service: extendedsupport.ServiceRDS, CostAmounts: extendedsupport.CostAmounts{Amount: 2500}},
				}
				r.Rows = []extendedsupport.CostRow{
					{StartDate: "2026-10-01", EndDate: "2026-10-01", Service: extendedsupport.ServiceRDS, CostAmounts: extendedsupport.CostAmounts{Amount: 1200}},
					{StartDate: "2026-10-01", EndDate: "2026-10-01", Service: extendedsupport.ServiceEKS, CostAmounts: extendedsupport.CostAmounts{Amount: 6}},
					{StartDate: "2026-11-01", EndDate: "2026-11-01", Service: extendedsupport.ServiceRDS, CostAmounts: extendedsupport.CostAmounts{Amount: 1300}},
					{StartDate: "2026-11-01", EndDate: "2026-11-01", Service: extendedsupport.ServiceEKS, CostAmounts: extendedsupport.CostAmounts{Amount: 6}},
				}
				if granularity == "monthly" {
					r.Rows[0].EndDate = "2026-10-31"
					r.Rows[1].EndDate = "2026-10-31"
				}
				if grouping != extendedsupport.CostGroupByService {
					for i := range r.Rows {
						r.Rows[i].AccountID = "111111111111"
						if r.Rows[i].Service == extendedsupport.ServiceEKS {
							r.Rows[i].AccountID = "222222222222"
						}
						if grouping == extendedsupport.CostGroupByAccount {
							r.Rows[i].Service = ""
						}
					}
				}
				var buf bytes.Buffer
				if err := WriteExtendedSupportCostResult(&buf, FormatPrettyPrint, r); err != nil {
					t.Fatal(err)
				}
				sections := strings.Split(buf.String(), "\nTime breakdown\n")
				if len(sections) != 2 {
					t.Fatalf("missing totals or breakdown: %s", &buf)
				}
				totals := strings.Split(sections[0], "Totals by service for full period\n")
				if len(totals) != 2 {
					t.Fatalf("missing service totals: %s", &buf)
				}
				for _, want := range []string{"SERVICE", "eks", "rds", "USD 12.00", "USD 2,500.00"} {
					if !strings.Contains(totals[1], want) {
						t.Fatalf("service totals missing %q: %s", want, totals[1])
					}
				}
				for _, want := range []string{"FROM", "TO", "2026-10-01", "2026-11-01", "USD 1,200.00", "USD 1,300.00"} {
					if !strings.Contains(sections[1], want) {
						t.Fatalf("breakdown missing %q: %s", want, sections[1])
					}
				}
				if strings.Contains(totals[1], "ACCOUNT") || strings.Contains(totals[1], "2026-") || strings.Contains(sections[1], "USD 2,500.00") {
					t.Fatalf("totals and breakdown were mixed: %s", &buf)
				}
				if (grouping != extendedsupport.CostGroupByService) != strings.Contains(sections[1], "111111111111") {
					t.Fatalf("account grouping changed: %s", sections[1])
				}
			})
		}
	}
}

type supportCostFailWriter struct{}

func (supportCostFailWriter) Write([]byte) (int, error) { return 0, errors.New("write failed") }
