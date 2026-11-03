package output

import (
	"bytes"
	"strings"
	"testing"

	"github.com/openshift-online/finops-tools/core/accountreview"
	"github.com/openshift-online/finops-tools/core/extendedsupport"
	"github.com/openshift-online/finops-tools/core/snapshot"
)

func TestResourceScanOutputShowsIntentionalExclusions(t *testing.T) {
	excluded := []string{"us-west-2"}
	details := []accountreview.AccountDetails{{AccountID: "111111111111", ExcludedRegions: excluded}}
	for _, format := range []Format{FormatPrettyPrint, FormatJSON} {
		for _, tc := range []struct {
			name  string
			write func(*bytes.Buffer) error
		}{
			{name: "snapshots", write: func(w *bytes.Buffer) error {
				return WriteSnapshotListResult(w, format, snapshot.Result{Summary: snapshot.Summary{ExcludedRegions: excluded}})
			}},
			{name: "extended support", write: func(w *bytes.Buffer) error {
				return WriteExtendedSupportListResult(w, format, extendedsupport.Result{Summary: extendedsupport.Summary{ExcludedRegions: excluded}})
			}},
			{name: "account details", write: func(w *bytes.Buffer) error {
				return WriteAccountDetails(w, format, details)
			}},
		} {
			t.Run(tc.name+"/"+string(format), func(t *testing.T) {
				var buf bytes.Buffer
				if err := tc.write(&buf); err != nil {
					t.Fatal(err)
				}
				if !strings.Contains(buf.String(), "us-west-2") || strings.Contains(buf.String(), "Skipped regions") || strings.Contains(buf.String(), "skipped_regions") || strings.Contains(buf.String(), "None found (scanned)") {
					t.Fatal(buf.String())
				}
				if tc.name == "account details" && format == FormatJSON && strings.Contains(buf.String(), `"vpcs": 0`) {
					t.Fatal("excluded regions must not produce account-wide zero counts", buf.String())
				}
			})
		}
	}
	var csv bytes.Buffer
	if err := WriteAccountDetails(&csv, FormatCSV, details); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(csv.String(), ",excluded_region,us-west-2,") || strings.Contains(csv.String(), ",count,") {
		t.Fatal(csv.String())
	}
}
