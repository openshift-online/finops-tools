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

func TestWriteExtendedSupportSeparatesPastSupportFromUnknown(t *testing.T) {
	for _, current := range []bool{false, true} {
		t.Run(map[bool]string{false: "no current findings", true: "with current findings"}[current], func(t *testing.T) {
			r := fixtureExtendedSupportResult()
			past := r.Records[0]
			past.ResourceID = "db-expired"
			r.Summary.PastExtendedSupportResources = []extendedsupport.Record{past}
			r.Summary.UnclassifiedResources = []extendedsupport.ResourceWarning{{
				AccountID: past.AccountID, Region: past.Region, Service: past.Service, ResourceType: past.ResourceType,
				ResourceID: "db-unknown", Message: "lifecycle information is missing",
			}}
			if !current {
				r.Records = nil
				r.Summary.TotalCount = 0
				r.Summary.ByService = nil
				r.Summary.ByAccount = nil
			}
			var pretty bytes.Buffer
			if err := WriteExtendedSupportListResult(&pretty, FormatPrettyPrint, r); err != nil {
				t.Fatal(err)
			}
			parts := strings.Split(pretty.String(), "\nPast extended support\n")
			if len(parts) != 2 || !strings.Contains(parts[0], "Support status unknown") || !strings.Contains(parts[0], "db-unknown") || strings.Contains(parts[0], "db-expired") || !strings.Contains(parts[1], "db-expired") || strings.Contains(parts[1], "db-unknown") {
				t.Fatalf("expired and unknown resources are not separated:\n%s", &pretty)
			}
			for _, want := range []string{"Resources past extended support:  1", "EXT SUPPORT ENDS", "2026-12-31"} {
				if !strings.Contains(pretty.String(), want) {
					t.Fatalf("missing %q:\n%s", want, &pretty)
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
			if !strings.Contains(encoded.String(), `"past_extended_support_resources"`) || len(decoded.Summary.PastExtendedSupportResources) != 1 || decoded.Summary.PastExtendedSupportResources[0].ResourceID != "db-expired" || !decoded.Summary.PastExtendedSupportResources[0].ExtendedSupportEndDate.Equal(*past.ExtendedSupportEndDate) || len(decoded.Summary.UnclassifiedResources) != 1 || decoded.Summary.TotalCount != len(r.Records) {
				t.Fatalf("JSON = %s", &encoded)
			}
			var csv bytes.Buffer
			if err := WriteExtendedSupportListResult(&csv, FormatCSV, r); err != nil {
				t.Fatal(err)
			}
			if strings.Contains(csv.String(), "db-expired") || strings.Contains(csv.String(), "db-unknown") || strings.Contains(csv.String(), "db-old") != current {
				t.Fatalf("CSV must contain only current extended-support findings: %s", &csv)
			}
		})
	}
}

func TestWriteExtendedSupportEndOfLifeWithoutSupportDates(t *testing.T) {
	endOfLife := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	r := extendedsupport.Result{Summary: extendedsupport.Summary{
		PastExtendedSupportResources: []extendedsupport.Record{{
			AccountID: "111111111111", Region: "us-east-1", Service: extendedsupport.ServiceOpenSearch,
			ResourceType: extendedsupport.ResourceTypeOpenSearchDomain, ResourceID: "domain-eol",
			Engine: "elasticsearch", EngineVersion: "5.1", EndOfLifeDate: &endOfLife,
		}},
	}}
	var pretty bytes.Buffer
	if err := WriteExtendedSupportListResult(&pretty, FormatPrettyPrint, r); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Past extended support", "domain-eol", "END OF LIFE", "2026-10-22", "—"} {
		if !strings.Contains(pretty.String(), want) {
			t.Fatalf("missing %q in %s", want, &pretty)
		}
	}
	if strings.Contains(pretty.String(), "0001-01-01") || strings.Contains(pretty.String(), "Support status unknown") {
		t.Fatalf("end of life resource has invalid output: %s", &pretty)
	}
	var encoded bytes.Buffer
	if err := WriteExtendedSupportListResult(&encoded, FormatJSON, r); err != nil {
		t.Fatal(err)
	}
	var decoded extendedsupport.Result
	if err := json.Unmarshal(encoded.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	past := decoded.Summary.PastExtendedSupportResources
	if len(past) != 1 || past[0].EndOfLifeDate == nil || !past[0].EndOfLifeDate.Equal(endOfLife) || decoded.Summary.TotalCount != 0 || len(decoded.Summary.UnclassifiedResources) != 0 {
		t.Fatalf("JSON = %s", &encoded)
	}
	for _, unwanted := range []string{"standard_support_end_date", "extended_support_end_date", "0001-01-01"} {
		if strings.Contains(encoded.String(), unwanted) {
			t.Fatalf("JSON fabricated %q: %s", unwanted, &encoded)
		}
	}
	if !strings.Contains(encoded.String(), `"end_of_life_date"`) {
		t.Fatal(encoded.String())
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

func TestWriteExtendedSupportLifecycleFailuresRemainVisibleWithoutFindings(t *testing.T) {
	result := extendedsupport.Result{Summary: extendedsupport.Summary{LifecycleWarnings: []string{"AWS Health catalog: SubscriptionRequiredException"}}}
	for _, format := range []Format{FormatPrettyPrint, FormatJSON} {
		var out bytes.Buffer
		if err := WriteExtendedSupportListResult(&out, format, result); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out.String(), "SubscriptionRequiredException") {
			t.Fatal(out.String())
		}
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

func TestWriteExtendedSupportEmptyBillingSelectionExplainsCoverage(t *testing.T) {
	result := extendedsupport.Result{Summary: extendedsupport.Summary{BillingSelection: &extendedsupport.BillingSelection{
		PayerAccountID: "123456789012", Start: "2026-09-20", EndExclusive: "2026-10-20",
		UsageTypes: []string{}, EligibleAccountCount: 250, SelectedAccountCount: 0,
		Notice: "Billing data can be delayed; accounts without matching recorded usage in this window are not scanned.",
	}}}
	var pretty bytes.Buffer
	if err := WriteExtendedSupportListResult(&pretty, FormatPrettyPrint, result); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Cost Explorer", "0 of 250", "2026-09-20", "end exclusive", "Billing data can be delayed", "not scanned", "Resources in extended support:"} {
		if !strings.Contains(pretty.String(), want) {
			t.Errorf("missing %q: %s", want, &pretty)
		}
	}
	if strings.Contains(pretty.String(), "Skipped accounts") {
		t.Fatal("filtered accounts were reported as scan failures")
	}
	var encoded bytes.Buffer
	if err := WriteExtendedSupportListResult(&encoded, FormatJSON, result); err != nil {
		t.Fatal(err)
	}
	var decoded extendedsupport.Result
	if err := json.Unmarshal(encoded.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Summary.BillingSelection == nil || decoded.Summary.BillingSelection.EligibleAccountCount != 250 || decoded.Summary.BillingSelection.SelectedAccountCount != 0 || len(decoded.Summary.SkippedAccounts) != 0 {
		t.Fatal(decoded)
	}
}

func TestWriteExtendedSupportIncludesAllServicesInEveryFormat(t *testing.T) {
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
	cache := r.Records[0]
	cache.Service = extendedsupport.ServiceElastiCache
	cache.ResourceType = extendedsupport.ResourceTypeElastiCacheCluster
	cache.ResourceID = "cache-old"
	cache.Engine = "redis"
	cache.EngineVersion = "5.0.6"
	cache.MajorEngineVersion = "5"
	cache.EngineLifecycleSupport = ""
	r.Records = append(r.Records, cache)
	domain := r.Records[0]
	domain.Service = extendedsupport.ServiceOpenSearch
	domain.ResourceType = extendedsupport.ResourceTypeOpenSearchDomain
	domain.ResourceID = "domain-old"
	domain.Engine = "elasticsearch"
	domain.EngineVersion = "7.8"
	domain.MajorEngineVersion = "7.8"
	domain.EngineLifecycleSupport = ""
	r.Records = append(r.Records, domain)
	r.Summary.TotalCount = 4
	r.Summary.ByService = append(r.Summary.ByService, extendedsupport.ServiceSummary{Service: extendedsupport.ServiceEKS, Count: 1})
	r.Summary.ByService = append(r.Summary.ByService, extendedsupport.ServiceSummary{Service: extendedsupport.ServiceElastiCache, Count: 1})
	r.Summary.ByService = append(r.Summary.ByService, extendedsupport.ServiceSummary{Service: extendedsupport.ServiceOpenSearch, Count: 1})
	r.Summary.SkippedRegions = []extendedsupport.RegionWarning{{AccountID: "222222222222", Region: "us-west-2", Service: extendedsupport.ServiceEKS, Message: "EKS list denied"}}
	r.Summary.SkippedRegions = append(r.Summary.SkippedRegions, extendedsupport.RegionWarning{AccountID: "222222222222", Region: "us-west-2", Service: extendedsupport.ServiceElastiCache, Message: "ElastiCache list denied"})
	r.Summary.SkippedRegions = append(r.Summary.SkippedRegions, extendedsupport.RegionWarning{AccountID: "222222222222", Region: "us-west-2", Service: extendedsupport.ServiceOpenSearch, Message: "OpenSearch list denied"})
	r.Summary.UnclassifiedResources = []extendedsupport.ResourceWarning{{AccountID: "111111111111", Region: "us-east-1", Service: extendedsupport.ServiceEKS, ResourceType: extendedsupport.ResourceTypeEKSCluster, ResourceID: "eks-unknown", Message: "version metadata missing"}}
	r.Summary.UnclassifiedResources = append(r.Summary.UnclassifiedResources, extendedsupport.ResourceWarning{AccountID: "111111111111", Region: "us-east-1", Service: extendedsupport.ServiceElastiCache, ResourceType: extendedsupport.ResourceTypeElastiCacheCluster, ResourceID: "cache-unknown", Message: "cache lifecycle missing"})
	r.Summary.UnclassifiedResources = append(r.Summary.UnclassifiedResources, extendedsupport.ResourceWarning{AccountID: "111111111111", Region: "us-east-1", Service: extendedsupport.ServiceOpenSearch, ResourceType: extendedsupport.ResourceTypeOpenSearchDomain, ResourceID: "domain-unknown", Message: "OpenSearch lifecycle missing"})
	for _, format := range []Format{FormatPrettyPrint, FormatJSON, FormatCSV} {
		t.Run(string(format), func(t *testing.T) {
			var out bytes.Buffer
			if err := WriteExtendedSupportListResult(&out, format, r); err != nil {
				t.Fatal(err)
			}
			for _, want := range []string{"eks-old", "eks-cluster", "kubernetes", "1.31", "db-old", "cache-old", "elasticache-cluster", "redis", "5.0.6", "domain-old", "opensearch-domain", "elasticsearch", "7.8"} {
				if !strings.Contains(out.String(), want) {
					t.Fatalf("missing %q: %s", want, out.String())
				}
			}
			if format == FormatCSV {
				if strings.Contains(out.String(), "eks-unknown") || strings.Contains(out.String(), "cache-unknown") || strings.Contains(out.String(), "domain-unknown") || len(strings.Split(strings.TrimSpace(out.String()), "\n")) != 5 {
					t.Fatal(out.String())
				}
			} else {
				for _, want := range []string{"eks-unknown", "EKS list denied", "cache-unknown", "ElastiCache list denied", "domain-unknown", "OpenSearch list denied"} {
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
				if decoded.Summary.SkippedRegions[1].Service != extendedsupport.ServiceElastiCache || decoded.Summary.UnclassifiedResources[1].Service != extendedsupport.ServiceElastiCache || decoded.Records[2].Service != extendedsupport.ServiceElastiCache {
					t.Fatal(decoded)
				}
				if decoded.Summary.SkippedRegions[2].Service != extendedsupport.ServiceOpenSearch || decoded.Summary.UnclassifiedResources[2].Service != extendedsupport.ServiceOpenSearch || decoded.Records[3].Service != extendedsupport.ServiceOpenSearch {
					t.Fatal(decoded)
				}
			}
		})
	}
}
