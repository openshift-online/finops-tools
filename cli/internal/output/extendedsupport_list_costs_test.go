package output

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/openshift-online/finops-tools/core/extendedsupport"
)

func TestExtendedSupportListChargesAppearOnlyInSummaries(t *testing.T) {
	result := fixtureExtendedSupportResult()
	amount := 123.45
	charges := &extendedsupport.SupportCharges{Amount: &amount, Currency: "USD"}
	result.Summary.ByAccount[0].SupportCost = charges
	result.Summary.ByService[0].SupportCost = charges
	result.Summary.Costs = &extendedsupport.ListCostSummary{
		Start: "2026-10-10", EndExclusive: "2026-11-09", Metric: "NetUnblendedCost",
		Notice: "Historical charges may include upgraded or deleted resources.",
	}
	var pretty bytes.Buffer
	if err := WriteExtendedSupportListResult(&pretty, FormatPrettyPrint, result); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"By account", "By service", "RESOURCES", "2026-10-10", "2026-11-09 (end exclusive)", "Historical charges may include upgraded or deleted resources."} {
		if !strings.Contains(pretty.String(), want) {
			t.Fatalf("missing %q in %s", want, &pretty)
		}
	}
	if strings.Count(pretty.String(), "EXT SUPPORT CHARGES") != 2 || strings.Count(pretty.String(), "USD 123.45") != 2 {
		t.Fatal(pretty.String())
	}
	parts := strings.Split(pretty.String(), "\nResources in extended support\n")
	if len(parts) != 2 || strings.Contains(parts[1], "EXT SUPPORT CHARGES") || strings.Contains(parts[1], "123.45") || !strings.Contains(parts[1], "db-old") {
		t.Fatal("account charges leaked into resource details", pretty.String())
	}
	var encoded bytes.Buffer
	if err := WriteExtendedSupportListResult(&encoded, FormatJSON, result); err != nil {
		t.Fatal(err)
	}
	var decoded extendedsupport.Result
	if err := json.Unmarshal(encoded.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	if *decoded.Summary.ByAccount[0].SupportCost.Amount != amount || *decoded.Summary.ByService[0].SupportCost.Amount != amount || decoded.Summary.Costs.Start != "2026-10-10" {
		t.Fatal(encoded.String())
	}
	var raw struct{ Records []map[string]any }
	if err := json.Unmarshal(encoded.Bytes(), &raw); err != nil {
		t.Fatal(err)
	}
	if _, ok := raw.Records[0]["support_cost"]; ok {
		t.Fatal("resource has an account cost", encoded.String())
	}
	var csv bytes.Buffer
	if err := WriteExtendedSupportListResult(&csv, FormatCSV, result); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(csv.String(), "support_cost") || strings.Contains(csv.String(), "123.45") || !strings.Contains(csv.String(), "db-old") {
		t.Fatal("CSV must remain resource inventory", csv.String())
	}
}

func TestExtendedSupportListChargesDistinguishUnavailableAndZero(t *testing.T) {
	result := fixtureExtendedSupportResult()
	zero := 0.0
	result.Summary.ByAccount[0].SupportCost = &extendedsupport.SupportCharges{Amount: &zero, Currency: "USD"}
	result.Summary.ByService[0].SupportCost = &extendedsupport.SupportCharges{Message: "Account billing unavailable"}
	result.Summary.Costs = &extendedsupport.ListCostSummary{Warnings: []string{"billing source denied"}}
	var pretty bytes.Buffer
	if err := WriteExtendedSupportListResult(&pretty, FormatPrettyPrint, result); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"USD 0.00", "Unavailable", "billing source denied"} {
		if !strings.Contains(pretty.String(), want) {
			t.Fatalf("missing %q in %s", want, &pretty)
		}
	}
	var encoded bytes.Buffer
	if err := WriteExtendedSupportListResult(&encoded, FormatJSON, result); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(encoded.String(), `"amount": null`) || !strings.Contains(encoded.String(), `"amount": 0`) {
		t.Fatal(encoded.String())
	}
}

func TestExtendedSupportListCostsAbsentWithoutEnrichment(t *testing.T) {
	for _, format := range []Format{FormatPrettyPrint, FormatJSON, FormatCSV} {
		var out bytes.Buffer
		if err := WriteExtendedSupportListResult(&out, format, fixtureExtendedSupportResult()); err != nil {
			t.Fatal(err)
		}
		for _, unwanted := range []string{"support_cost", `"costs"`, "EXT SUPPORT CHARGES"} {
			if strings.Contains(out.String(), unwanted) {
				t.Fatal("unexpected cost output without enrichment", out.String())
			}
		}
	}
}
