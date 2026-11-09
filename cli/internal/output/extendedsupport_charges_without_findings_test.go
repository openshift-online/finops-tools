package output

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/openshift-online/finops-tools/core/extendedsupport"
)

func TestChargesWithoutFindingsVisibleWhenNoResourcesWereFound(t *testing.T) {
	result := extendedsupport.Result{Summary: extendedsupport.Summary{ChargesWithoutFindings: &extendedsupport.ChargesWithoutFindingsSummary{
		ListCostSummary: extendedsupport.ListCostSummary{Start: "2026-11-04", EndExclusive: "2026-11-09", Metric: "NetUnblendedCost", AllServices: true, Services: []extendedsupport.Service{}, Notice: "Charges cover all services, including services not implemented in discovery."},
		Accounts:        []extendedsupport.AccountChargesWithoutFindings{{AccountID: "111111111111", DisplayAlias: "empty-account", Amount: 12.34, Currency: "USD"}, {AccountID: "222222222222", Amount: 5, Currency: "USD", ScanIncomplete: true}},
	}}}
	var pretty bytes.Buffer
	if err := WriteExtendedSupportListResult(&pretty, FormatPrettyPrint, result); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Recent charges with unknown Extended Support resources", "2026-11-04 to 2026-11-09 (end exclusive)", "LAST 5 DAYS CHARGES", "111111111111", "222222222222", "empty-account", "USD 12.34", "USD 5.00", "See scan warnings", "all services"} {
		if !strings.Contains(pretty.String(), want) {
			t.Fatalf("missing %q in %s", want, &pretty)
		}
	}
	if strings.Contains(pretty.String(), "By account") || strings.Contains(pretty.String(), "Resources in extended support\n") {
		t.Fatal("billing-only accounts were added to resource findings")
	}
	var encoded bytes.Buffer
	if err := WriteExtendedSupportListResult(&encoded, FormatJSON, result); err != nil {
		t.Fatal(err)
	}
	var decoded extendedsupport.Result
	if err := json.Unmarshal(encoded.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	if len(decoded.Records) != 0 || decoded.Summary.TotalCount != 0 || len(decoded.Summary.ChargesWithoutFindings.Accounts) != 2 || decoded.Summary.ChargesWithoutFindings.Accounts[0].Amount != 12.34 || !decoded.Summary.ChargesWithoutFindings.Accounts[1].ScanIncomplete || !decoded.Summary.ChargesWithoutFindings.AllServices || !strings.Contains(encoded.String(), `"services": []`) {
		t.Fatal(encoded.String())
	}
	var csv bytes.Buffer
	if err := WriteExtendedSupportListResult(&csv, FormatCSV, result); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(csv.String(), "111111111111") || strings.Contains(csv.String(), "12.34") {
		t.Fatal("billing-only account leaked into resource CSV")
	}
}

func TestChargesWithoutFindingsWarningsRemainVisibleAndEmptyChecksStayQuiet(t *testing.T) {
	for _, hasWarnings := range []bool{true, false} {
		check := &extendedsupport.ChargesWithoutFindingsSummary{Accounts: []extendedsupport.AccountChargesWithoutFindings{}}
		if hasWarnings {
			check.Warnings = []string{"Cost Explorer denied the five-day check"}
		}
		var pretty bytes.Buffer
		if err := WriteExtendedSupportListResult(&pretty, FormatPrettyPrint, extendedsupport.Result{Summary: extendedsupport.Summary{ChargesWithoutFindings: check}}); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(pretty.String(), "Recent charges with unknown Extended Support resources") != hasWarnings || strings.Contains(pretty.String(), "Cost Explorer denied") != hasWarnings {
			t.Fatal(pretty.String())
		}
	}
}
