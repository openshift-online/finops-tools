package report

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/openshift-online/finops-tools/core/cost"
	corehcp "github.com/openshift-online/finops-tools/core/hcphierarchy"
	"github.com/openshift-online/finops-tools/core/report/costanomalies"
	execsum "github.com/openshift-online/finops-tools/core/report/execsummary"
	"github.com/openshift-online/finops-tools/core/report/savingsplans"
	"github.com/openshift-online/finops-tools/core/telesense"
)

// --- Validate() ---

func TestExecutiveSummaryGenerator_ValidateRequiresBothOpeners(t *testing.T) {
	validIn := GenerateInput{
		Format:          FormatHTML,
		Targets:         execSummaryTestTargets(),
		MappingCSVPath:  "mapping.csv",
		InfraAccountIDs: []string{"999999999999"},
	}

	if err := newExecutiveSummaryGenerator(nil, fakeSFOpener).Validate(validIn); err == nil {
		t.Fatal("expected error when mart opener is nil")
	}
	if err := newExecutiveSummaryGenerator(fakeSFOpener, nil).Validate(validIn); err == nil {
		t.Fatal("expected error when telesense opener is nil")
	}
}

func TestExecutiveSummaryGenerator_ValidateRequiresMappingCSV(t *testing.T) {
	gen := newExecutiveSummaryGenerator(fakeSFOpener, fakeSFOpener)
	err := gen.Validate(GenerateInput{
		Format:          FormatHTML,
		Targets:         execSummaryTestTargets(),
		InfraAccountIDs: []string{"999999999999"},
	})
	if err == nil || !strings.Contains(err.Error(), "mapping-csv") {
		t.Fatalf("err = %v, want mapping-csv error", err)
	}
}

func TestExecutiveSummaryGenerator_ValidateRequiresInfraAccountIDs(t *testing.T) {
	gen := newExecutiveSummaryGenerator(fakeSFOpener, fakeSFOpener)
	err := gen.Validate(GenerateInput{
		Format:         FormatHTML,
		Targets:        execSummaryTestTargets(),
		MappingCSVPath: "mapping.csv",
	})
	if err == nil || !strings.Contains(err.Error(), "infra-account-ids") {
		t.Fatalf("err = %v, want infra-account-ids error", err)
	}
}

func TestExecutiveSummaryGenerator_ValidateRequiresAllThreePayerAliases(t *testing.T) {
	gen := newExecutiveSummaryGenerator(fakeSFOpener, fakeSFOpener)
	err := gen.Validate(GenerateInput{
		Format: FormatHTML,
		Targets: []cost.AccountTarget{
			{AccountID: "123456789012", DisplayAlias: "rh-control"},
		},
		MappingCSVPath:  "mapping.csv",
		InfraAccountIDs: []string{"999999999999"},
	})
	if err == nil {
		t.Fatal("expected error for missing payer aliases")
	}
	if !strings.Contains(err.Error(), "osd-staging-1") || !strings.Contains(err.Error(), "osd-staging-2") {
		t.Fatalf("err = %v, want both missing aliases named", err)
	}
}

func TestExecutiveSummaryGenerator_ValidateRejectsUnsupportedFormat(t *testing.T) {
	gen := newExecutiveSummaryGenerator(fakeSFOpener, fakeSFOpener)
	err := gen.Validate(GenerateInput{
		Format:          "pdf",
		Targets:         execSummaryTestTargets(),
		MappingCSVPath:  "mapping.csv",
		InfraAccountIDs: []string{"999999999999"},
	})
	if err == nil {
		t.Fatal("expected format error")
	}
}

// --- execSummaryPayerTargets ---

func TestExecSummaryPayerTargets_MapsByDisplayAlias(t *testing.T) {
	got, err := execSummaryPayerTargets(execSummaryTestTargets())
	if err != nil {
		t.Fatalf("execSummaryPayerTargets() error = %v", err)
	}
	if got[execsum.PayerProduction].AccountID != "123456789012" {
		t.Errorf("production account = %q", got[execsum.PayerProduction].AccountID)
	}
	if got[execsum.PayerStaging].AccountID != "987654321098" {
		t.Errorf("staging account = %q", got[execsum.PayerStaging].AccountID)
	}
	if got[execsum.PayerIntegration].AccountID != "444444444444" {
		t.Errorf("integration account = %q", got[execsum.PayerIntegration].AccountID)
	}
}

// --- execSummaryWindow / monthLabelsFor ---

func TestExecSummaryWindow_DerivesMonthsFromDateRange(t *testing.T) {
	dr := cost.DateRange{
		Start: time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC),
		End:   time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), // exclusive
	}
	window := execSummaryWindow(dr)
	if len(window.Months) != 2 {
		t.Fatalf("len(Months) = %d, want 2", len(window.Months))
	}
	labels := monthLabelsFor(window.Months)
	if labels[0] != "2026-04" || labels[1] != "2026-05" {
		t.Fatalf("labels = %v, want [2026-04 2026-05]", labels)
	}
}

// --- conversion helpers ---

func TestConvertClusterDetailInputs_RegionFallbackChain(t *testing.T) {
	rows := []corehcp.HierarchyRow{
		{CustomerClusterID: "c1", CustomerRegion: "us-east-1", MCRegion: "us-west-2"},
		{CustomerClusterID: "c2", MCRegion: "us-west-2"},
		{CustomerClusterID: "c3", SCRegion: "eu-west-1"},
		{CustomerClusterID: ""}, // no cluster ID: dropped
	}
	got := convertClusterDetailInputs(rows)
	if len(got) != 3 {
		t.Fatalf("len(got) = %d, want 3", len(got))
	}
	if got[0].Region != "us-east-1" {
		t.Errorf("c1 region = %q, want us-east-1 (own region wins)", got[0].Region)
	}
	if got[1].Region != "us-west-2" {
		t.Errorf("c2 region = %q, want us-west-2 (MC fallback)", got[1].Region)
	}
	if got[2].Region != "eu-west-1" {
		t.Errorf("c3 region = %q, want eu-west-1 (SC fallback)", got[2].Region)
	}
}

func TestHCPAccountIDSet_CollectsMCAndSCAccounts(t *testing.T) {
	rows := []corehcp.HierarchyRow{
		{MCAccountID: "111111111111", SCAccountID: "222222222222"},
		{MCAccountID: "111111111111"}, // dup MC, no SC
	}
	got := hcpAccountIDSet(rows)
	if len(got) != 2 || !got["111111111111"] || !got["222222222222"] {
		t.Fatalf("got = %v", got)
	}
}

func TestClusterCountsByEnvironment_FiltersByEnvironment(t *testing.T) {
	rows := []corehcp.ClusterCountRow{
		{Month: "2026-04", Environment: "Production", NewClusters: 2},
		{Month: "2026-05", Environment: "Production", NewClusters: 3},
		{Month: "2026-04", Environment: "Stage", NewClusters: 10},
	}
	got := clusterCountsByEnvironment(rows, "Production")
	if got["2026-04"] != 2 || got["2026-05"] != 3 {
		t.Fatalf("got = %v", got)
	}
	if _, ok := got["2026-04-stage-leak"]; ok {
		t.Fatal("unexpected key")
	}
}

func TestDistinctProductTypes_PreservesFirstSeenOrderAndDedupes(t *testing.T) {
	rows := []corehcp.ClusterGrowthRow{
		{ProductType: "ROSA HCP"},
		{ProductType: "OSD"},
		{ProductType: "ROSA HCP"},
	}
	got := distinctProductTypes(rows)
	if len(got) != 2 || got[0] != "ROSA HCP" || got[1] != "OSD" {
		t.Fatalf("got = %v", got)
	}
}

func TestMcSummaryForMonth_FiltersSortsAndTruncates(t *testing.T) {
	all := []execsum.MCSummaryRecord{
		{MCName: "mc-a", Month: "2026-04", MCCost: 100},
		{MCName: "mc-b", Month: "2026-05", MCCost: 999}, // different month: excluded
		{MCName: "mc-c", Month: "2026-04", MCCost: 300},
	}
	got := mcSummaryForMonth(all, "2026-04", 1)
	if len(got) != 1 || got[0].MCName != "mc-c" {
		t.Fatalf("got = %+v, want top-1 mc-c (highest MCCost in 2026-04)", got)
	}
}

func TestTruncateMLAnomalies_SortsByImpactDescendingAndCaps(t *testing.T) {
	list := []execsum.MLAnomalyRecord{
		{Service: "low", TotalImpact: 10},
		{Service: "high", TotalImpact: 1000},
		{Service: "mid", TotalImpact: 100},
	}
	got := truncateMLAnomalies(list, 2)
	if len(got) != 2 || got[0].Service != "high" || got[1].Service != "mid" {
		t.Fatalf("got = %+v", got)
	}
}

func TestTruncateAnomalyRecords_CapsWithoutResorting(t *testing.T) {
	list := []execsum.AnomalyRecord{{AccountID: "a"}, {AccountID: "b"}, {AccountID: "c"}}
	got := truncateAnomalyRecords(list, 2)
	if len(got) != 2 || got[0].AccountID != "a" || got[1].AccountID != "b" {
		t.Fatalf("got = %+v", got)
	}
}

func TestBuildSPMonthPoints_MergesByMonth(t *testing.T) {
	coverage := []execsum.SPMonthlyMetric{{Month: "2026-04", Percentage: 70}}
	utilization := []execsum.SPMonthlyMetric{{Month: "2026-05", Percentage: 90}}
	savings := []execsum.SPMonthlySavings{{Month: "2026-04", NetSavings: 50, OnDemandCostEquivalent: 200}}

	got := buildSPMonthPoints(coverage, utilization, savings)
	if len(got) != 2 {
		t.Fatalf("len(got) = %d, want 2", len(got))
	}
	if got[0].Month != "2026-04" || got[0].CoveragePercentage == nil || *got[0].CoveragePercentage != 70 {
		t.Errorf("got[0] = %+v", got[0])
	}
	if got[0].UtilizationPercentage != nil {
		t.Errorf("got[0].UtilizationPercentage = %v, want nil (no April utilization data)", *got[0].UtilizationPercentage)
	}
	if got[1].Month != "2026-05" || got[1].UtilizationPercentage == nil || *got[1].UtilizationPercentage != 90 {
		t.Errorf("got[1] = %+v", got[1])
	}
}

// --- fakes shared by tests in this file ---

func execSummaryTestTargets() []cost.AccountTarget {
	return []cost.AccountTarget{
		{AccountID: "123456789012", DisplayAlias: "rh-control"},
		{AccountID: "987654321098", DisplayAlias: "osd-staging-1"},
		{AccountID: "444444444444", DisplayAlias: "osd-staging-2"},
	}
}

type fakeExecSummarySFQueryer struct{}

func (fakeExecSummarySFQueryer) QueryContext(context.Context, string, ...any) (corehcp.SnowflakeRows, error) {
	return nil, nil
}

func (fakeExecSummarySFQueryer) Close() error { return nil }

func fakeSFOpener(context.Context, string, string) (corehcp.SnowflakeQueryer, error) {
	return fakeExecSummarySFQueryer{}, nil
}

// --- full Generate() smoke test ---

func TestExecutiveSummaryGenerator_GenerateEndToEndSmoke(t *testing.T) {
	origHierarchy := execSummaryHierarchyBuild
	origCounts := execSummaryClusterCountsBuild
	origGrowth := execSummaryClusterGrowthBuild
	origTelesense := execSummaryTelesenseBuild
	origCostFetch := execSummaryCostFetch
	origSP := execSummarySavingsPlansBuild
	origAnomalies := execSummaryCostAnomaliesBuild
	t.Cleanup(func() {
		execSummaryHierarchyBuild = origHierarchy
		execSummaryClusterCountsBuild = origCounts
		execSummaryClusterGrowthBuild = origGrowth
		execSummaryTelesenseBuild = origTelesense
		execSummaryCostFetch = origCostFetch
		execSummarySavingsPlansBuild = origSP
		execSummaryCostAnomaliesBuild = origAnomalies
	})

	execSummaryHierarchyBuild = func(context.Context, corehcp.SnowflakeQueryer, string) (corehcp.Report, error) {
		return corehcp.Report{
			Rows: []corehcp.HierarchyRow{
				{
					CustomerClusterID: "cluster-1",
					CustomerRegion:    "us-east-1",
					MCName:            "mc-1",
					MCRegion:          "us-east-1",
					MCAccountID:       "111111111111",
					SCName:            "sc-1",
					SCAccountID:       "222222222222",
					SCRegion:          "us-east-1",
					Classification:    "external",
					OrganizationName:  "org-1",
					BillingModel:      "standard",
				},
			},
		}, nil
	}
	execSummaryClusterCountsBuild = func(context.Context, corehcp.SnowflakeQueryer, string) (corehcp.ClusterCountsReport, error) {
		return corehcp.ClusterCountsReport{
			Counts: []corehcp.ClusterCountRow{
				{Month: "2026-04", Environment: "Production", NewClusters: 1},
				{Month: "2026-04", Environment: "Stage", NewClusters: 1},
				{Month: "2026-04", Environment: "Integration", NewClusters: 1},
			},
			ActiveByEnv: []corehcp.EnvironmentActiveCount{{Environment: "Production", ActiveCount: 1}},
		}, nil
	}
	execSummaryClusterGrowthBuild = func(context.Context, corehcp.SnowflakeQueryer, string) (corehcp.ClusterGrowthReport, error) {
		return corehcp.ClusterGrowthReport{
			Rows: []corehcp.ClusterGrowthRow{{Month: "2026-04", ProductType: "ROSA HCP", NewClusters: 1}},
		}, nil
	}
	execSummaryTelesenseBuild = func(context.Context, telesense.SnowflakeQueryer, []string, time.Time, time.Time, string) (telesense.Report, error) {
		return telesense.Report{
			Rows: []telesense.ClusterMonthUsage{
				{ClusterID: "cluster-1", Month: "2026-04", VCPUHours: 500},
				{ClusterID: "cluster-1", Month: "2026-05", VCPUHours: 600},
			},
		}, nil
	}
	execSummaryCostFetch = func(_ context.Context, q cost.CostQuery) ([]cost.MonthlyAccountServiceCost, error) {
		acct := q.Accounts[0].AccountID
		return []cost.MonthlyAccountServiceCost{
			{AccountID: "111111111111", Service: "Amazon EC2", Month: "2026-04", Amount: 1000, Currency: "USD"},
			{AccountID: "111111111111", Service: "Amazon EC2", Month: "2026-05", Amount: 1200, Currency: "USD"},
			{AccountID: acct, Service: "Amazon S3", Month: "2026-04", Amount: 50, Currency: "USD"},
		}, nil
	}
	execSummarySavingsPlansBuild = func(_ context.Context, accounts []cost.AccountTarget, _ cost.DateRange) (savingsplans.Report, error) {
		return savingsplans.Report{
			Accounts: []savingsplans.AccountReport{{
				AccountID:   accounts[0].AccountID,
				Coverage:    []savingsplans.MonthlyMetric{{Month: "2026-04", Percentage: 70}, {Month: "2026-05", Percentage: 75}},
				Utilization: []savingsplans.MonthlyMetric{{Month: "2026-04", Percentage: 90}, {Month: "2026-05", Percentage: 95}},
				Savings:     []savingsplans.MonthlySavings{{Month: "2026-04", NetSavings: 50, OnDemandCostEquivalent: 200}},
			}},
		}, nil
	}
	execSummaryCostAnomaliesBuild = func(_ context.Context, accounts []cost.AccountTarget, _ cost.DateRange) (costanomalies.Report, error) {
		return costanomalies.Report{
			Anomalies: []costanomalies.Anomaly{{
				Service: "Amazon EC2", TotalImpact: 500, ActualSpend: 1000, ExpectedSpend: 500,
				StartDate: "2026-05-01", EndDate: "2026-05-02",
			}},
		}, nil
	}

	mappingCSV := filepath.Join(t.TempDir(), "mapping.csv")
	writeExecSummaryMappingCSV(t, mappingCSV)

	var buf bytes.Buffer
	gen := newExecutiveSummaryGenerator(fakeSFOpener, fakeSFOpener)
	in := GenerateInput{
		Format:  FormatHTML,
		Out:     &buf,
		Targets: execSummaryTestTargets(),
		Range: cost.DateRange{
			Start: time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC),
			End:   time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
		},
		Progress:        noopStepper{},
		Now:             time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC),
		MappingCSVPath:  mappingCSV,
		InfraAccountIDs: []string{"999999999999"},
	}

	if err := gen.Validate(in); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if err := gen.Generate(context.Background(), in); err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	html := buf.String()
	for _, want := range []string{"Production", "Staging", "Integration", "All Payers", "HCP Cost Efficiency", "Cluster Growth"} {
		if !strings.Contains(html, want) {
			t.Errorf("rendered HTML missing %q", want)
		}
	}
}

func writeExecSummaryMappingCSV(t *testing.T, path string) {
	t.Helper()
	content := "Account ID,Account name,Service Name,service-phase,cost-center,app-code\n" +
		"111111111111,MC Account,HCP Infra,prod,cc-1,app-1\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write mapping csv: %v", err)
	}
}
