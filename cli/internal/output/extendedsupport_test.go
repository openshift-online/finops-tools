package output

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/openshift-online/finops-tools/core/extendedsupport"
)

func fixtureExtendedSupportResult() extendedsupport.Result {
	standardEnd := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	extendedEnd := time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)
	return extendedsupport.Result{
		Records: []extendedsupport.Record{{
			AccountID:              "111111111111",
			Region:                 "us-east-1",
			Service:                extendedsupport.ServiceRDS,
			ResourceType:           extendedsupport.ResourceTypeRDSInstance,
			ResourceID:             "db-old",
			Engine:                 "postgres",
			EngineVersion:          "15.4",
			MajorEngineVersion:     "15",
			StandardSupportEndDate: standardEnd,
			ExtendedSupportEndDate: &extendedEnd,
			EngineLifecycleSupport: "open-source-rds-extended-support",
		}},
		Summary: extendedsupport.Summary{
			TotalCount: 1,
			ByService:  []extendedsupport.ServiceSummary{{Service: extendedsupport.ServiceRDS, Count: 1}},
			ByAccount:  []extendedsupport.AccountSummary{{AccountID: "111111111111", Count: 1}},
		},
	}
}

func TestWriteExtendedSupportReportsUnknownResources(t *testing.T) {
	r := fixtureExtendedSupportResult()
	r.Summary.UnclassifiedResources = []extendedsupport.ResourceWarning{{
		AccountID: "111111111111", Region: "us-east-1", ResourceType: extendedsupport.ResourceTypeRDSInstance,
		ResourceID: "db-unknown", Engine: "postgres", EngineVersion: "11.1", Message: "standard support end date is missing",
	}}
	var pretty bytes.Buffer
	if err := WriteExtendedSupportListResult(&pretty, FormatPrettyPrint, r); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Support status unknown", "db-unknown", "standard support end date is missing", "db-old"} {
		if !strings.Contains(pretty.String(), want) {
			t.Fatalf("missing %q in %s", want, pretty.String())
		}
	}
	var encoded bytes.Buffer
	if err := WriteExtendedSupportListResult(&encoded, FormatJSON, r); err != nil {
		t.Fatal(err)
	}
	var decoded extendedsupport.Result
	if err := json.Unmarshal(encoded.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	if len(decoded.Summary.UnclassifiedResources) != 1 || decoded.Summary.UnclassifiedResources[0].ResourceID != "db-unknown" || decoded.Summary.TotalCount != 1 || len(decoded.Records) != 1 {
		t.Fatalf("decoded = %+v", decoded)
	}
	// Unknown support status must remain visible when no resources qualify.
	r.Records = nil
	r.Summary.TotalCount = 0
	pretty.Reset()
	if err := WriteExtendedSupportListResult(&pretty, FormatPrettyPrint, r); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(pretty.String(), "db-unknown") {
		t.Fatal(pretty.String())
	}
}

func TestWriteExtendedSupportListResultCSV(t *testing.T) {
	buf := new(bytes.Buffer)
	if err := WriteExtendedSupportListResult(buf, FormatCSV, fixtureExtendedSupportResult()); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("lines = %d, want 2", len(lines))
	}
	if !strings.Contains(lines[0], "standard_support_end_date") {
		t.Fatalf("header = %q", lines[0])
	}
	if !strings.Contains(lines[1], "db-old") || !strings.Contains(lines[1], "2026-12-31") {
		t.Fatalf("row = %q", lines[1])
	}
}

func TestWriteExtendedSupportListResultJSON(t *testing.T) {
	buf := new(bytes.Buffer)
	if err := WriteExtendedSupportListResult(buf, FormatJSON, fixtureExtendedSupportResult()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), `"total_count": 1`) {
		t.Fatalf("output = %s", buf.String())
	}
}

func TestWriteExtendedSupportListResultPretty(t *testing.T) {
	buf := new(bytes.Buffer)
	if err := WriteExtendedSupportListResult(buf, FormatPrettyPrint, fixtureExtendedSupportResult()); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"Resources in extended support:",
		"By service",
		"db-old",
		"STD SUPPORT ENDED",
		"2026-12-31",
	} {
		if !strings.Contains(buf.String(), want) {
			t.Fatalf("output missing %q:\n%s", want, buf.String())
		}
	}
}
