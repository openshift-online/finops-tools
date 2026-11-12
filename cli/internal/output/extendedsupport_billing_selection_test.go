package output

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/openshift-online/finops-tools/core/extendedsupport"
)

func TestBillingSelectionSanitizesAccountNameAndAlias(t *testing.T) {
	for _, tc := range []struct {
		name        string
		displayName string
		want        string
	}{
		{name: "alias only", want: "member"},
		{name: "name and alias", displayName: "\x1b]0;injected-title\x07Member One", want: "Member One (member)"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, usageTypes := range [][]string{nil, {"RDS:ExtendedSupport"}} {
				selection := &extendedsupport.BillingSelection{
					SelectedAccounts: []extendedsupport.BillingSelectedAccount{{
						AccountID: "111111111111", DisplayName: tc.displayName,
						DisplayAlias: "member\x1b[2J\x1b[H", UsageTypes: usageTypes,
					}},
				}
				var pretty bytes.Buffer
				if err := writeBillingSelectionDetails(&pretty, styler{}, selection); err != nil {
					t.Fatal(err)
				}
				got := pretty.String()
				if strings.ContainsAny(got, "\x1b\x07") || strings.Contains(got, "injected-title") {
					t.Fatalf("terminal escape leaked into output: %q", got)
				}
				if !strings.Contains(got, tc.want) {
					t.Fatalf("missing sanitized name %q in %q", tc.want, got)
				}
			}
		})
	}
}

func TestBillingSelectionListsExactUsageTypesEvenWithoutResourceFindings(t *testing.T) {
	selection := &extendedsupport.BillingSelection{
		PayerAccountID: "123456789012", PayerAccountIDs: []string{"123456789012"}, Start: "2026-10-11", EndExclusive: "2026-11-10",
		UsageTypes:           []string{"RDS:ExtendedSupport", "USE1-AmazonEKS-Hours:extendedSupport", "UPPER-EXTENDEDSUPPORT"},
		EligibleAccountCount: 10, SelectedAccountCount: 2, BilledAccountCount: 4,
		SelectedAccounts: []extendedsupport.BillingSelectedAccount{
			{AccountID: "111111111111", DisplayName: "Member One", DisplayAlias: "one", UsageTypes: []string{"RDS:ExtendedSupport", "UPPER-EXTENDEDSUPPORT"}},
			{AccountID: "222222222222", UsageTypes: []string{"USE1-AmazonEKS-Hours:extendedSupport"}},
		},
	}
	result := extendedsupport.Result{Summary: extendedsupport.Summary{BillingSelection: selection, SkippedAccounts: []extendedsupport.AccountWarning{{AccountID: "222222222222", Message: "scan failed after selection"}}}}
	var pretty bytes.Buffer
	if err := WriteExtendedSupportListResult(&pretty, FormatPrettyPrint, result); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Matched Extended Support usage types", "Accounts selected for scanning from Cost Explorer", "MATCHED USAGE TYPE", "Member One (one)", "111111111111", "222222222222", "RDS:ExtendedSupport", "USE1-AmazonEKS-Hours:extendedSupport", "UPPER-EXTENDEDSUPPORT", "2 of 10", "2026-10-11 to 2026-11-10", "scan failed after selection"} {
		if !strings.Contains(pretty.String(), want) {
			t.Fatalf("missing %q in %s", want, &pretty)
		}
	}
	parts := strings.Split(pretty.String(), "Accounts selected for scanning from Cost Explorer\n")
	if len(parts) != 2 {
		t.Fatal(pretty.String())
	}
	audit := strings.Split(parts[1], "Resources in extended support:")[0]
	if strings.Count(audit, "111111111111") != 2 || strings.Count(audit, "222222222222") != 1 {
		t.Fatal("selected-account usage mapping is not preserved", audit)
	}
	var encoded bytes.Buffer
	if err := WriteExtendedSupportListResult(&encoded, FormatJSON, result); err != nil {
		t.Fatal(err)
	}
	var decoded extendedsupport.Result
	if err := json.Unmarshal(encoded.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	if len(decoded.Records) != 0 || !reflect.DeepEqual(decoded.Summary.BillingSelection.SelectedAccounts, selection.SelectedAccounts) || !reflect.DeepEqual(decoded.Summary.BillingSelection.UsageTypes, selection.UsageTypes) {
		t.Fatal(encoded.String())
	}
	var csv bytes.Buffer
	if err := WriteExtendedSupportListResult(&csv, FormatCSV, result); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(csv.String(), "111111111111") || strings.Contains(csv.String(), "RDS:ExtendedSupport") {
		t.Fatal("billing selection leaked into resource CSV")
	}
}

func TestBillingSelectionEmptyAccountsStillListsDiscoveredUsageTypes(t *testing.T) {
	result := extendedsupport.Result{Summary: extendedsupport.Summary{BillingSelection: &extendedsupport.BillingSelection{
		UsageTypes: []string{"RDS:ExtendedSupport"}, SelectedAccounts: []extendedsupport.BillingSelectedAccount{}, EligibleAccountCount: 10,
	}}}
	var pretty bytes.Buffer
	if err := WriteExtendedSupportListResult(&pretty, FormatPrettyPrint, result); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"0 of 10", "RDS:ExtendedSupport", "Accounts selected for scanning from Cost Explorer\n  None"} {
		if !strings.Contains(pretty.String(), want) {
			t.Fatalf("missing %q in %s", want, &pretty)
		}
	}
	var encoded bytes.Buffer
	if err := WriteExtendedSupportListResult(&encoded, FormatJSON, result); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(encoded.String(), `"selected_accounts": []`) {
		t.Fatal(encoded.String())
	}
}

func TestBillingSelectionAuditAbsentWhenBillingFilterIsBypassed(t *testing.T) {
	var pretty bytes.Buffer
	if err := WriteExtendedSupportListResult(&pretty, FormatPrettyPrint, fixtureExtendedSupportResult()); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(pretty.String(), "Accounts selected for scanning from Cost Explorer") || strings.Contains(pretty.String(), "Matched Extended Support usage types") {
		t.Fatal(pretty.String())
	}
}
