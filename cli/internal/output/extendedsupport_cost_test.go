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
	return extendedsupport.CostResult{
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
	if strings.Contains(text, "FROM") || strings.Contains(text, "TO ") || strings.Contains(text, "ESTIMATED") || strings.Contains(text, "AVOIDABLE SURCHARGES") || strings.Count(text, "2026-10-01") != 1 {
		t.Fatalf("unexpected totals table columns: %s", text)
	}
}

type supportCostFailWriter struct{}

func (supportCostFailWriter) Write([]byte) (int, error) { return 0, errors.New("write failed") }
