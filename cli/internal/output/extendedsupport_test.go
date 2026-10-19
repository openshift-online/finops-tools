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

func TestWriteExtendedSupportIncludesEKSInEveryFormat(t *testing.T) {
	r := fixtureExtendedSupportResult()
	cluster := r.Records[0]
	cluster.Service = extendedsupport.ServiceEKS
	cluster.ResourceType = extendedsupport.ResourceTypeEKSCluster
	cluster.ResourceID = "eks-old"
	cluster.Engine = "kubernetes"
	cluster.EngineVersion = "1.31"
	cluster.MajorEngineVersion = "1.31"
	cluster.EngineLifecycleSupport = "EXTENDED"
	r.Records = append(r.Records, cluster)
	r.Summary.TotalCount = 2
	r.Summary.ByService = append(r.Summary.ByService, extendedsupport.ServiceSummary{Service: extendedsupport.ServiceEKS, Count: 1})
	r.Summary.SkippedRegions = []extendedsupport.RegionWarning{{AccountID: "222222222222", Region: "us-west-2", Service: extendedsupport.ServiceEKS, Message: "EKS list denied"}}
	r.Summary.UnclassifiedResources = []extendedsupport.ResourceWarning{{AccountID: "111111111111", Region: "us-east-1", Service: extendedsupport.ServiceEKS, ResourceType: extendedsupport.ResourceTypeEKSCluster, ResourceID: "eks-unknown", Message: "version metadata missing"}}
	for _, format := range []Format{FormatPrettyPrint, FormatJSON, FormatCSV} {
		t.Run(string(format), func(t *testing.T) {
			var out bytes.Buffer
			if err := WriteExtendedSupportListResult(&out, format, r); err != nil {
				t.Fatal(err)
			}
			for _, want := range []string{"eks-old", "eks-cluster", "kubernetes", "1.31", "db-old"} {
				if !strings.Contains(out.String(), want) {
					t.Fatalf("missing %q: %s", want, out.String())
				}
			}
			if format == FormatCSV {
				if strings.Contains(out.String(), "eks-unknown") || len(strings.Split(strings.TrimSpace(out.String()), "\n")) != 3 {
					t.Fatal(out.String())
				}
			} else {
				for _, want := range []string{"eks-unknown", "EKS list denied"} {
					if !strings.Contains(out.String(), want) {
						t.Fatal(out.String())
					}
				}
			}
			if format == FormatJSON {
				var decoded extendedsupport.Result
				if err := json.Unmarshal(out.Bytes(), &decoded); err != nil {
					t.Fatal(err)
				}
				if decoded.Summary.SkippedRegions[0].Service != extendedsupport.ServiceEKS || decoded.Summary.UnclassifiedResources[0].Service != extendedsupport.ServiceEKS || decoded.Records[1].EngineLifecycleSupport != "EXTENDED" {
					t.Fatal(decoded)
				}
			}
		})
	}
}
