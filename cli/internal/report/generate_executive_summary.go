package report

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/openshift-online/finops-tools/core/cost"
	corehcp "github.com/openshift-online/finops-tools/core/hcphierarchy"
	"github.com/openshift-online/finops-tools/core/report/costanomalies"
	execsum "github.com/openshift-online/finops-tools/core/report/execsummary"
	"github.com/openshift-online/finops-tools/core/report/savingsplans"
	"github.com/openshift-online/finops-tools/core/telesense"
)

// execSummaryPayerDef pairs one of the executive-summary report's 3
// registered AWS account aliases (golang-executive-summary-spec.md section
// 4, "no new AWS targeting flags needed") with its spec section 7 payer tab
// identity. These are internal finops aliases, not AWS account IDs --
// acceptable in source per .cursor/rules/aws-account-placeholders.mdc.
type execSummaryPayerDef struct {
	Alias string
	ID    execsum.PayerID
	Label string
}

var execSummaryPayerDefs = []execSummaryPayerDef{
	{Alias: "rh-control", ID: execsum.PayerProduction, Label: "Production"},
	{Alias: "osd-staging-1", ID: execsum.PayerStaging, Label: "Staging"},
	{Alias: "osd-staging-2", ID: execsum.PayerIntegration, Label: "Integration"},
}

// execSummaryEnvironmentForPayer maps hcphierarchy's OCM_MAPPING.ENVIRONMENT
// values to the matching payer tab (spec section 5 step 3 / 6.11's
// per-environment cluster-count denominator). This is a separate dimension
// from the AWS payer aliases above -- by convention the two line up 1:1,
// but nothing here assumes the same account IDs.
var execSummaryEnvironmentForPayer = map[execsum.PayerID]string{
	execsum.PayerProduction:  "Production",
	execsum.PayerStaging:     "Stage",
	execsum.PayerIntegration: "Integration",
}

// execSummaryTopN* are the spec's fixed table sizes for growing accounts /
// per-account top services (spec 6.8), distinct from anomalyDisplayLimit
// (spec 7.4, executive_summary_accounts.go), which this file reuses as-is
// for MLAnomalies/StatisticalAnomalies truncation.
const (
	execSummaryTopGrowingAccounts = 5
	execSummaryTopServicesPerAcct = 3
	execSummaryMCSummaryLimit     = 25
	execSummaryClusterDetailLimit = 100
)

// Package-level function variables for the executive-summary generator's
// I/O dependencies, overridable in tests (mirrors generate_costs.go's
// reportOrganizationRootID / reportBuildOUAccountMapping pattern).
var (
	execSummaryHierarchyBuild     = corehcp.Build
	execSummaryClusterCountsBuild = corehcp.BuildClusterCounts
	execSummaryClusterGrowthBuild = corehcp.BuildClusterGrowth
	execSummaryTelesenseBuild     = telesense.Build
	execSummaryCostFetch          = cost.FetchMonthlyByAccountAndService
	execSummarySavingsPlansBuild  = savingsplans.Build
	execSummaryCostAnomaliesBuild = costanomalies.Build
)

type executiveSummaryGenerator struct {
	martOpener      SnowflakeMartOpener
	telesenseOpener SnowflakeTelesenseOpener
}

func newExecutiveSummaryGenerator(martOpener SnowflakeMartOpener, telesenseOpener SnowflakeTelesenseOpener) executiveSummaryGenerator {
	return executiveSummaryGenerator{martOpener: martOpener, telesenseOpener: telesenseOpener}
}

func (g executiveSummaryGenerator) Validate(in GenerateInput) error {
	if err := validateTemplateFormat(TemplateExecutiveSummary, in.Format); err != nil {
		return err
	}
	if g.martOpener == nil {
		return fmt.Errorf("executive-summary report: snowflake mart opener not configured")
	}
	if g.telesenseOpener == nil {
		return fmt.Errorf("executive-summary report: snowflake telesense opener not configured")
	}
	if strings.TrimSpace(in.MappingCSVPath) == "" {
		return fmt.Errorf("executive-summary report requires --mapping-csv")
	}
	if len(in.InfraAccountIDs) == 0 {
		return fmt.Errorf("executive-summary report requires --infra-account-ids")
	}
	if _, err := execSummaryPayerTargets(in.Targets); err != nil {
		return err
	}
	return nil
}

func (g executiveSummaryGenerator) Generate(ctx context.Context, in GenerateInput) error {
	payerTargets, err := execSummaryPayerTargets(in.Targets)
	if err != nil {
		return err
	}

	mapping, err := execsum.LoadMappingCSVFile(in.MappingCSVPath)
	if err != nil {
		return fmt.Errorf("load mapping csv: %w", err)
	}

	window := execSummaryWindow(in.Range)
	if len(window.Months) < 2 {
		return fmt.Errorf("executive-summary report needs at least 2 months in the reporting window (got %d); widen --months", len(window.Months))
	}
	monthLabels := monthLabelsFor(window.Months)
	lastMonth := monthLabels[len(monthLabels)-1]
	prevMonth := monthLabels[len(monthLabels)-2]

	in.Progress.Step("Resolving HCP hierarchy from Snowflake mart…")
	sfMart, err := g.martOpener(ctx, in.ConfigPath, in.SnowflakeAlias)
	if err != nil {
		return err
	}
	defer func() { _ = sfMart.Close() }()

	hierarchy, err := execSummaryHierarchyBuild(ctx, sfMart, "")
	if err != nil {
		return err
	}
	clusterCounts, err := execSummaryClusterCountsBuild(ctx, sfMart, "")
	if err != nil {
		return err
	}
	clusterGrowth, err := execSummaryClusterGrowthBuild(ctx, sfMart, "")
	if err != nil {
		return err
	}

	in.Progress.Step("Fetching vCPU-hour usage from Telesense…")
	sfTelesense, err := g.telesenseOpener(ctx, in.ConfigPath, in.SnowflakeProdAlias)
	if err != nil {
		return err
	}
	defer func() { _ = sfTelesense.Close() }()

	windowEnd := window.End.AddDate(0, 1, 0)
	usage, err := execSummaryTelesenseBuild(ctx, sfTelesense, hierarchy.ClusterIDs(), window.Start, windowEnd, "")
	if err != nil {
		return err
	}
	vcpu := convertClusterMonthVCPU(usage.Rows)

	var (
		allEnriched []execsum.EnrichedCostRecord
		allRawCosts []execsum.CostRecord

		enrichedByPayer   = make(map[execsum.PayerID][]execsum.EnrichedCostRecord, len(execSummaryPayerDefs))
		mlByPayer         = make(map[execsum.PayerID][]execsum.MLAnomalyRecord, len(execSummaryPayerDefs))
		coverageByPayer   = make(map[execsum.PayerID][]execsum.SPMonthlyMetric, len(execSummaryPayerDefs))
		utilByPayer       = make(map[execsum.PayerID][]execsum.SPMonthlyMetric, len(execSummaryPayerDefs))
		savingsByPayer    = make(map[execsum.PayerID][]execsum.SPMonthlySavings, len(execSummaryPayerDefs))
		hasSPDataByPayer  = make(map[execsum.PayerID]bool, len(execSummaryPayerDefs))
		cumulativeByPayer = make(map[execsum.PayerID]map[string]int, len(execSummaryPayerDefs))
	)

	for _, pd := range execSummaryPayerDefs {
		target := payerTargets[pd.ID]

		in.Progress.Step(fmt.Sprintf("Fetching costs for %s (%s)…", pd.Label, pd.Alias))
		rows, err := execSummaryCostFetch(ctx, cost.CostQuery{
			Accounts: []cost.AccountTarget{target},
			Range:    in.Range,
			Workers:  in.Workers,
		})
		if err != nil {
			return fmt.Errorf("fetch costs for payer %s: %w", pd.Label, err)
		}
		records := convertCostRecords(rows, string(pd.ID))
		allRawCosts = append(allRawCosts, records...)
		enriched := execsum.EnrichCostRecords(records, mapping)
		enrichedByPayer[pd.ID] = enriched
		allEnriched = append(allEnriched, enriched...)

		in.Progress.Step(fmt.Sprintf("Fetching Savings Plans data for %s…", pd.Label))
		spReport, err := execSummarySavingsPlansBuild(ctx, []cost.AccountTarget{target}, in.Range)
		if err != nil {
			return fmt.Errorf("fetch savings plans for payer %s: %w", pd.Label, err)
		}
		if len(spReport.Accounts) > 0 {
			acct := spReport.Accounts[0]
			coverage := convertMonthlyMetrics(acct.Coverage)
			utilization := convertMonthlyMetrics(acct.Utilization)
			coverageByPayer[pd.ID] = coverage
			utilByPayer[pd.ID] = utilization
			savingsByPayer[pd.ID] = convertMonthlySavings(acct.Savings)
			hasSPDataByPayer[pd.ID] = len(coverage) > 0 || len(utilization) > 0
		}

		if !in.NoAnomalies {
			in.Progress.Step(fmt.Sprintf("Fetching AWS cost anomalies for %s…", pd.Label))
			anomReport, err := execSummaryCostAnomaliesBuild(ctx, []cost.AccountTarget{target}, in.Range)
			if err != nil {
				return fmt.Errorf("fetch cost anomalies for payer %s: %w", pd.Label, err)
			}
			mlByPayer[pd.ID] = convertMLAnomalies(anomReport.Anomalies, string(pd.ID))
		}

		newByMonth := clusterCountsByEnvironment(clusterCounts.Counts, execSummaryEnvironmentForPayer[pd.ID])
		cumulativeByPayer[pd.ID] = execsum.CumulativeCounts(newByMonth, monthLabels)
	}

	hcpAccountIDs := hcpAccountIDSet(hierarchy.Rows)

	in.Progress.Step("Computing report analytics…")

	payerReports := make([]execsum.PayerReport, 0, len(execSummaryPayerDefs))
	var (
		mlAllFull        []execsum.MLAnomalyRecord
		coverageAll      [][]execsum.SPMonthlyMetric
		utilAll          [][]execsum.SPMonthlyMetric
		savingsAll       [][]execsum.SPMonthlySavings
		cumulativeAll    []map[string]int
		anyPayerHasSPAll bool
	)
	for _, pd := range execSummaryPayerDefs {
		payerReports = append(payerReports, buildPayerReport(
			pd.ID, pd.Label,
			enrichedByPayer[pd.ID],
			mlByPayer[pd.ID],
			coverageByPayer[pd.ID], utilByPayer[pd.ID], savingsByPayer[pd.ID],
			hasSPDataByPayer[pd.ID],
			hcpAccountIDs, cumulativeByPayer[pd.ID],
			window, lastMonth, prevMonth, in.NoAnomalies,
		))
		mlAllFull = append(mlAllFull, mlByPayer[pd.ID]...)
		coverageAll = append(coverageAll, coverageByPayer[pd.ID])
		utilAll = append(utilAll, utilByPayer[pd.ID])
		savingsAll = append(savingsAll, savingsByPayer[pd.ID])
		cumulativeAll = append(cumulativeAll, cumulativeByPayer[pd.ID])
		if hasSPDataByPayer[pd.ID] {
			anyPayerHasSPAll = true
		}
	}

	allPayerReport := buildPayerReport(
		execsum.PayerAll, "All Payers",
		allEnriched,
		mlAllFull,
		execsum.CombinePercentageAcrossPayers(coverageAll),
		execsum.CombinePercentageAcrossPayers(utilAll),
		execsum.CombineSavingsAcrossPayers(savingsAll),
		anyPayerHasSPAll,
		hcpAccountIDs, execsum.CombineCumulativeCounts(cumulativeAll, monthLabels),
		window, lastMonth, prevMonth, in.NoAnomalies,
	)

	in.Progress.Step("Computing HCP Cost Efficiency and Cluster Growth analytics…")

	regionInputs := convertClusterRegionInputs(hierarchy.Rows)
	regionMetrics := execsum.ComputeRegionMonthMetrics(regionInputs, vcpu, allRawCosts, in.InfraAccountIDs, monthLabels)
	vcpuEff := execsum.ComputeVCPUEfficiency(regionInputs, vcpu, regionMetrics)
	mcInputs := convertClusterMCInputs(hierarchy.Rows)
	mcSummaryAll := execsum.ComputeMCSummary(mcInputs, vcpu, allRawCosts)
	clusterDetailInputs := convertClusterDetailInputs(hierarchy.Rows)

	hcpEfficiency := execsum.HCPEfficiencyReport{
		Months:                   monthLabels,
		Regions:                  regionMetrics,
		VCPUEfficiency:           vcpuEff,
		MCSummaryLatestMonth:     mcSummaryForMonth(mcSummaryAll, lastMonth, execSummaryMCSummaryLimit),
		ClusterDetailLatestMonth: execsum.BuildClusterDetailRecords(clusterDetailInputs, vcpu, lastMonth, execSummaryClusterDetailLimit),
	}

	growthInputs := convertClusterGrowthInputs(clusterGrowth.Rows)
	clusterGrowthTab := execsum.ClusterGrowthTabReport{
		Months:       monthLabels,
		ProductTypes: execsum.SortProductTypes(distinctProductTypes(clusterGrowth.Rows)),
		Points:       execsum.BuildClusterGrowthPoints(growthInputs, monthLabels),
	}

	report := execsum.Report{
		GeneratedAt: in.Now,
		Window:      window,
		DataSources: []string{
			"AWS Cost Explorer (3 payers)",
			"AWS Cost Anomaly Detection",
			"OCM_MAPPING (Snowflake)",
			"Telesense OCP_USAGE (Snowflake)",
			"Mapping CSV",
		},
		AllPayers:     allPayerReport,
		Payers:        payerReports,
		HCPEfficiency: hcpEfficiency,
		ClusterGrowth: clusterGrowthTab,
	}

	in.Progress.Step("Rendering HTML report…")
	return RenderExecutiveSummaryHTML(in.Out, report)
}

// execSummaryPayerTargets matches in.Targets (resolved from
// --account-alias) against execSummaryPayerDefs by DisplayAlias, returning
// an error naming any missing payer alias.
func execSummaryPayerTargets(targets []cost.AccountTarget) (map[execsum.PayerID]cost.AccountTarget, error) {
	byAlias := make(map[string]cost.AccountTarget, len(targets))
	for _, t := range targets {
		if alias := strings.TrimSpace(t.DisplayAlias); alias != "" {
			byAlias[alias] = t
		}
	}

	out := make(map[execsum.PayerID]cost.AccountTarget, len(execSummaryPayerDefs))
	var missing []string
	for _, pd := range execSummaryPayerDefs {
		t, ok := byAlias[pd.Alias]
		if !ok {
			missing = append(missing, pd.Alias)
			continue
		}
		out[pd.ID] = t
	}
	if len(missing) > 0 {
		names := make([]string, len(execSummaryPayerDefs))
		for i, pd := range execSummaryPayerDefs {
			names[i] = pd.Alias
		}
		return nil, fmt.Errorf(
			"executive-summary report requires --account-alias %s (missing: %s)",
			strings.Join(names, ","), strings.Join(missing, ", "),
		)
	}
	return out, nil
}

// execSummaryWindow derives an execsummary.TimeWindow from the resolved
// cost query date range (in.Range), rather than recomputing "N months ago"
// independently -- this keeps the AWS cost fetch window and the report's
// month labels as a single source of truth.
func execSummaryWindow(dr cost.DateRange) execsum.TimeWindow {
	lastIncluded := dr.End.AddDate(0, 0, -1)
	months := execsum.MonthsInWindow(dr.Start, lastIncluded)
	if len(months) == 0 {
		return execsum.TimeWindow{}
	}
	return execsum.TimeWindow{Start: months[0], End: months[len(months)-1], Months: months}
}

func monthLabelsFor(months []time.Time) []string {
	out := make([]string, len(months))
	for i, m := range months {
		out[i] = execsum.MonthLabel(m)
	}
	return out
}

// buildPayerReport assembles one tab's PayerReport (spec section 7) from
// already-fetched/converted per-payer (or cross-payer-combined, for the All
// Payers tab) inputs. coverage/utilization/savings are plain
// []SPMonthlyMetric/[]SPMonthlySavings regardless of whether the caller
// passes one payer's own data or the CombinePercentageAcrossPayers /
// CombineSavingsAcrossPayers output.
func buildPayerReport(
	id execsum.PayerID,
	label string,
	enriched []execsum.EnrichedCostRecord,
	ml []execsum.MLAnomalyRecord,
	coverage, utilization []execsum.SPMonthlyMetric,
	savings []execsum.SPMonthlySavings,
	hasSPData bool,
	hcpAccountIDs map[string]bool,
	clusterCumulative map[string]int,
	window execsum.TimeWindow,
	lastMonth, prevMonth string,
	noAnomalies bool,
) execsum.PayerReport {
	rawCosts := make([]execsum.CostRecord, len(enriched))
	for i, e := range enriched {
		rawCosts[i] = e.CostRecord
	}

	categoryMonths := execsum.CategoryMonthly(enriched)
	categoryOrder := execsum.CategoryOrderByTotalCost(categoryMonths)
	growing := execsum.TopGrowingAccounts(enriched, window.Months, execSummaryTopGrowingAccounts, execSummaryTopServicesPerAcct)

	var statAnomalies []execsum.AnomalyRecord
	if !noAnomalies {
		statAnomalies = execsum.StatisticalAnomalies(enriched, window.Months)
	}

	spCoverageAvg := execsum.AveragePercentage(coverage)
	spUtilizationAvg := execsum.AveragePercentage(utilization)
	netSavings, onDemandEquiv := execsum.TotalSavings(savings)

	kpis := execsum.ComputePayerKPIs(execsum.PayerKPIInputs{
		PayerLabel:               label,
		CostRecords:              rawCosts,
		LastMonth:                lastMonth,
		PrevMonth:                prevMonth,
		HCPAccountIDs:            hcpAccountIDs,
		ClusterCumulativeByMonth: clusterCumulative,
		SPCoverageAvg:            spCoverageAvg,
		SPUtilizationAvg:         spUtilizationAvg,
		TotalNetSavings:          netSavings,
		TotalOnDemandEquiv:       onDemandEquiv,
		AnomalyCount:             len(statAnomalies),
	})

	var spMonths []execsum.SPMonthPoint
	if hasSPData {
		spMonths = buildSPMonthPoints(coverage, utilization, savings)
	}

	return execsum.PayerReport{
		ID:                   id,
		Label:                label,
		KPIs:                 kpis,
		CategoryMonths:       categoryMonths,
		CategoryOrder:        categoryOrder,
		GrowingAccounts:      growing,
		MLAnomalies:          truncateMLAnomalies(ml, anomalyDisplayLimit),
		StatisticalAnomalies: truncateAnomalyRecords(statAnomalies, anomalyDisplayLimit),
		HasSPData:            hasSPData,
		SPMonths:             spMonths,
		SPNetSavingsWindow:   netSavings,
	}
}

// buildSPMonthPoints merges coverage/utilization/savings (all keyed by
// month) into one ordered []SPMonthPoint for the SP Trend section (spec 7.5).
func buildSPMonthPoints(coverage, utilization []execsum.SPMonthlyMetric, savings []execsum.SPMonthlySavings) []execsum.SPMonthPoint {
	coverageByMonth := make(map[string]float64, len(coverage))
	for _, m := range coverage {
		coverageByMonth[m.Month] = m.Percentage
	}
	utilByMonth := make(map[string]float64, len(utilization))
	for _, m := range utilization {
		utilByMonth[m.Month] = m.Percentage
	}
	savingsByMonth := make(map[string]float64, len(savings))

	monthSet := make(map[string]bool)
	for _, m := range coverage {
		monthSet[m.Month] = true
	}
	for _, m := range utilization {
		monthSet[m.Month] = true
	}
	for _, s := range savings {
		savingsByMonth[s.Month] = s.NetSavings
		monthSet[s.Month] = true
	}

	months := make([]string, 0, len(monthSet))
	for m := range monthSet {
		months = append(months, m)
	}
	sort.Strings(months)

	out := make([]execsum.SPMonthPoint, 0, len(months))
	for _, m := range months {
		p := execsum.SPMonthPoint{Month: m, NetSavings: savingsByMonth[m]}
		if v, ok := coverageByMonth[m]; ok {
			p.CoveragePercentage = floatPtr(v)
		}
		if v, ok := utilByMonth[m]; ok {
			p.UtilizationPercentage = floatPtr(v)
		}
		out = append(out, p)
	}
	return out
}

// truncateMLAnomalies sorts by TotalImpact descending (list may be a
// concatenation across payers with no pre-existing global order) and caps
// at limit.
func truncateMLAnomalies(list []execsum.MLAnomalyRecord, limit int) []execsum.MLAnomalyRecord {
	sorted := append([]execsum.MLAnomalyRecord(nil), list...)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].TotalImpact > sorted[j].TotalImpact
	})
	if len(sorted) > limit {
		sorted = sorted[:limit]
	}
	return sorted
}

// truncateAnomalyRecords caps an already-sorted-by-|z-score| list at limit
// (execsum.StatisticalAnomalies already sorts descending by |ZScore|).
func truncateAnomalyRecords(list []execsum.AnomalyRecord, limit int) []execsum.AnomalyRecord {
	if len(list) > limit {
		return list[:limit]
	}
	return list
}

// mcSummaryForMonth filters ComputeMCSummary's full output down to one
// month, sorted by MCCost descending and capped at limit (spec 7.6.4).
func mcSummaryForMonth(all []execsum.MCSummaryRecord, month string, limit int) []execsum.MCSummaryRecord {
	var out []execsum.MCSummaryRecord
	for _, r := range all {
		if r.Month == month {
			out = append(out, r)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].MCCost > out[j].MCCost
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

func distinctProductTypes(rows []corehcp.ClusterGrowthRow) []string {
	seen := make(map[string]bool)
	var out []string
	for _, r := range rows {
		if !seen[r.ProductType] {
			seen[r.ProductType] = true
			out = append(out, r.ProductType)
		}
	}
	return out
}

// --- Conversions from core/hcphierarchy, core/telesense, core/cost, and
// core/report/{savingsplans,costanomalies} types to the dependency-free
// core/report/execsummary input shapes (see execsummary's package doc:
// "this package has no dependency on hcphierarchy so it stays a pure,
// independently-testable formula layer"). ---

func convertClusterMonthVCPU(rows []telesense.ClusterMonthUsage) []execsum.ClusterMonthVCPU {
	out := make([]execsum.ClusterMonthVCPU, len(rows))
	for i, r := range rows {
		out[i] = execsum.ClusterMonthVCPU{ClusterID: r.ClusterID, Month: r.Month, VCPUHours: r.VCPUHours}
	}
	return out
}

func convertCostRecords(rows []cost.MonthlyAccountServiceCost, payer string) []execsum.CostRecord {
	out := make([]execsum.CostRecord, len(rows))
	for i, r := range rows {
		out[i] = execsum.CostRecord{
			Month:     r.Month,
			AccountID: r.AccountID,
			Cost:      r.Amount,
			Service:   r.Service,
			Payer:     payer,
		}
	}
	return out
}

func convertMonthlyMetrics(m []savingsplans.MonthlyMetric) []execsum.SPMonthlyMetric {
	out := make([]execsum.SPMonthlyMetric, len(m))
	for i, v := range m {
		out[i] = execsum.SPMonthlyMetric{Month: v.Month, Percentage: v.Percentage}
	}
	return out
}

func convertMonthlySavings(m []savingsplans.MonthlySavings) []execsum.SPMonthlySavings {
	out := make([]execsum.SPMonthlySavings, len(m))
	for i, v := range m {
		out[i] = execsum.SPMonthlySavings{Month: v.Month, NetSavings: v.NetSavings, OnDemandCostEquivalent: v.OnDemandCostEquivalent}
	}
	return out
}

func convertMLAnomalies(anoms []costanomalies.Anomaly, payer string) []execsum.MLAnomalyRecord {
	out := make([]execsum.MLAnomalyRecord, len(anoms))
	for i, a := range anoms {
		out[i] = execsum.MLAnomalyRecord{
			Payer:         payer,
			Service:       a.Service,
			TotalImpact:   a.TotalImpact,
			ActualSpend:   a.ActualSpend,
			ExpectedSpend: a.ExpectedSpend,
			StartDate:     a.StartDate,
			EndDate:       a.EndDate,
		}
	}
	return out
}

func convertClusterRegionInputs(rows []corehcp.HierarchyRow) []execsum.ClusterRegionInput {
	out := make([]execsum.ClusterRegionInput, 0, len(rows))
	for _, r := range rows {
		if r.CustomerClusterID == "" {
			continue
		}
		out = append(out, execsum.ClusterRegionInput{
			ClusterID:      r.CustomerClusterID,
			CustomerRegion: r.CustomerRegion,
			MCRegion:       r.MCRegion,
			SCRegion:       r.SCRegion,
			MCAccountID:    r.MCAccountID,
			SCAccountID:    r.SCAccountID,
			Classification: r.Classification,
		})
	}
	return out
}

func convertClusterMCInputs(rows []corehcp.HierarchyRow) []execsum.ClusterMCInput {
	out := make([]execsum.ClusterMCInput, 0, len(rows))
	for _, r := range rows {
		if r.CustomerClusterID == "" {
			continue
		}
		out = append(out, execsum.ClusterMCInput{
			ClusterID:      r.CustomerClusterID,
			MCName:         r.MCName,
			MCRegion:       r.MCRegion,
			MCAccountID:    r.MCAccountID,
			SCName:         r.SCName,
			Classification: r.Classification,
		})
	}
	return out
}

func convertClusterDetailInputs(rows []corehcp.HierarchyRow) []execsum.ClusterDetailInput {
	out := make([]execsum.ClusterDetailInput, 0, len(rows))
	for _, r := range rows {
		if r.CustomerClusterID == "" {
			continue
		}
		region := r.CustomerRegion
		if region == "" {
			region = r.MCRegion
		}
		if region == "" {
			region = r.SCRegion
		}
		out = append(out, execsum.ClusterDetailInput{
			ClusterID:      r.CustomerClusterID,
			MCName:         r.MCName,
			Region:         region,
			Classification: r.Classification,
			Organization:   r.OrganizationName,
			BillingModel:   r.BillingModel,
		})
	}
	return out
}

func convertClusterGrowthInputs(rows []corehcp.ClusterGrowthRow) []execsum.ClusterGrowthInput {
	out := make([]execsum.ClusterGrowthInput, len(rows))
	for i, r := range rows {
		out[i] = execsum.ClusterGrowthInput{Month: r.Month, ProductType: r.ProductType, NewClusters: r.NewClusters}
	}
	return out
}

func hcpAccountIDSet(rows []corehcp.HierarchyRow) map[string]bool {
	out := make(map[string]bool)
	for _, r := range rows {
		if r.MCAccountID != "" {
			out[r.MCAccountID] = true
		}
		if r.SCAccountID != "" {
			out[r.SCAccountID] = true
		}
	}
	return out
}

func clusterCountsByEnvironment(rows []corehcp.ClusterCountRow, environment string) map[string]int {
	out := make(map[string]int)
	for _, r := range rows {
		if r.Environment == environment {
			out[r.Month] += r.NewClusters
		}
	}
	return out
}
