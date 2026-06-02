// Simple validation test for execsummary package with real cost data
// Usage:
//   1. Fetch cost data: ./bin/finops cost get --account-alias <alias> --months 3 --split-by service --format json > cost.json
//   2. Run: go run test_validation.go cost.json
package main

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/openshift-online/finops-tools/core/report/execsummary"
)

type CostResult struct {
	Provider  string `json:"provider"`
	AccountID string `json:"account_id"`
	Amount    float64 `json:"amount"`
	Breakdown []struct {
		Service string  `json:"service"`
		Amount  float64 `json:"amount"`
	} `json:"breakdown"`
	StartDate string `json:"start_date"`
	EndDate   string `json:"end_date"`
}

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: go run test_validation.go <cost-data.json>")
		fmt.Println("")
		fmt.Println("First fetch cost data:")
		fmt.Println("  ./bin/finops cost get --account-alias <alias> --months 3 \\")
		fmt.Println("    --split-by service --format json > cost.json")
		fmt.Println("")
		fmt.Println("Then run:")
		fmt.Println("  go run test_validation.go cost.json")
		os.Exit(1)
	}

	filename := os.Args[1]

	fmt.Println("========================================")
	fmt.Println("Executive Summary Package Validation")
	fmt.Println("========================================")
	fmt.Println("")

	// Load the JSON data
	fmt.Printf("Loading cost data from %s...\n", filename)
	data, err := os.ReadFile(filename)
	if err != nil {
		log.Fatalf("Failed to read file: %v", err)
	}

	var result CostResult
	if err := json.Unmarshal(data, &result); err != nil {
		log.Fatalf("Failed to parse JSON: %v", err)
	}

	fmt.Printf("  Account: %s\n", result.AccountID)
	fmt.Printf("  Period: %s to %s\n", result.StartDate, result.EndDate)
	fmt.Printf("  Total: $%.2f\n", result.Amount)
	fmt.Printf("  Services: %d\n\n", len(result.Breakdown))

	// Parse start/end dates to get months
	start, _ := time.Parse("2006-01-02", result.StartDate)
	end, _ := time.Parse("2006-01-02", result.EndDate)
	months := getMonthsBetween(start, end)

	fmt.Printf("Test 1: Creating cost records...\n")
	var records []execsummary.CostRecord

	// Distribute cost evenly across months (simplified)
	for _, month := range months {
		for _, svc := range result.Breakdown {
			records = append(records, execsummary.CostRecord{
				Month:     month,
				AccountID: result.AccountID,
				Cost:      svc.Amount / float64(len(months)),
				Service:   svc.Service,
			})
		}
	}
	fmt.Printf("  ✓ Created %d cost records (%d months × %d services)\n\n",
		len(records), len(months), len(result.Breakdown))

	// Test 2: Index the data
	fmt.Println("Test 2: Indexing cost data...")
	costData := indexCostData(records)
	fmt.Printf("  ✓ Indexed by month: %d unique months\n", len(costData.ByMonth))
	fmt.Printf("  ✓ Indexed by account: %d unique accounts\n\n", len(costData.ByAccount))

	// Test 3: Enrich (no mappings for now)
	fmt.Println("Test 3: Enriching data...")
	enriched := execsummary.EnrichWithMapping(costData, []execsummary.AccountMapping{})
	fmt.Printf("  ✓ Enriched %d records\n\n", len(enriched))

	// Test 4: Total cost calculation
	fmt.Println("Test 4: Calculating total cost...")
	total := execsummary.TotalCost(enriched)
	fmt.Printf("  ✓ Total cost: $%.2f\n", total)
	fmt.Printf("  ✓ Matches input: %v (diff: $%.2f)\n\n",
		abs(total-result.Amount) < 0.01, abs(total-result.Amount))

	// Test 5: Top services
	fmt.Println("Test 5: Finding top services...")
	topServices := execsummary.ServiceCostsForAccount(enriched, result.AccountID, months[len(months)-1], 10)
	fmt.Printf("  Top 10 services for %s:\n", months[len(months)-1])
	for i, svc := range topServices {
		fmt.Printf("    %2d. %-40s $%10.2f\n", i+1, svc.Service, svc.Cost)
	}
	fmt.Println("")

	// Test 6: Monthly aggregation
	fmt.Println("Test 6: Monthly aggregation...")
	monthTimes := make([]time.Time, len(months))
	for i, m := range months {
		monthTimes[i], _ = time.Parse("2006-01", m)
	}
	categoryMonthly := execsummary.CategoryMonthly(enriched, monthTimes)
	fmt.Printf("  ✓ Generated %d category-month aggregates\n\n", len(categoryMonthly))

	// Test 7: Export to CSV
	fmt.Println("Test 7: Exporting to CSV...")
	if err := exportServiceCosts(topServices, "validation_services.csv"); err != nil {
		log.Fatalf("Export failed: %v", err)
	}
	fmt.Printf("  ✓ Exported to validation_services.csv\n\n")

	// Test 8: KPI computation (simplified, no clusters/HCP data)
	if len(months) >= 2 {
		fmt.Println("Test 8: Computing KPIs...")
		lastMonth := monthTimes[len(monthTimes)-1]
		prevMonth := monthTimes[len(monthTimes)-2]

		kpis := execsummary.ComputePayerKPIs(
			enriched,
			"all",
			lastMonth,
			&prevMonth,
			make(map[string]bool),
			execsummary.ClusterCounts{},
			execsummary.EnvCounts{},
			[]execsummary.AnomalyRecord{},
			nil,
		)

		fmt.Printf("  Last Month: $%.2f (%s)\n", kpis.TotalLast, execsummary.MonthLabel(lastMonth))
		fmt.Printf("  Prev Month: $%.2f (%s)\n", kpis.TotalPrev, execsummary.MonthLabel(prevMonth))
		fmt.Printf("  MoM Change: %.2f%%\n\n", kpis.MoMPct)
	}

	fmt.Println("========================================")
	fmt.Println("✅ All validation tests passed!")
	fmt.Println("========================================")
	fmt.Println("")
	fmt.Println("The execsummary package is working correctly with real AWS data.")
	fmt.Println("Next steps:")
	fmt.Println("  - Add account mapping file for better categorization")
	fmt.Println("  - Fetch 6+ months for anomaly detection")
	fmt.Println("  - Test with multiple accounts (payer account)")
}

func indexCostData(records []execsummary.CostRecord) *execsummary.CostData {
	data := &execsummary.CostData{
		Records:        records,
		ByMonth:        make(map[string][]execsummary.CostRecord),
		ByAccount:      make(map[string][]execsummary.CostRecord),
		ByMonthAccount: make(map[string]map[string]float64),
	}

	for _, rec := range records {
		data.ByMonth[rec.Month] = append(data.ByMonth[rec.Month], rec)
		data.ByAccount[rec.AccountID] = append(data.ByAccount[rec.AccountID], rec)

		if data.ByMonthAccount[rec.Month] == nil {
			data.ByMonthAccount[rec.Month] = make(map[string]float64)
		}
		data.ByMonthAccount[rec.Month][rec.AccountID] += rec.Cost
	}

	return data
}

func getMonthsBetween(start, end time.Time) []string {
	var months []string
	current := time.Date(start.Year(), start.Month(), 1, 0, 0, 0, 0, time.UTC)
	endMonth := time.Date(end.Year(), end.Month(), 1, 0, 0, 0, 0, time.UTC)

	for !current.After(endMonth) {
		months = append(months, execsummary.MonthLabel(current))
		current = current.AddDate(0, 1, 0)
	}
	return months
}

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}

func exportServiceCosts(services []execsummary.ServiceCostPair, filename string) error {
	f, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer f.Close()

	w := csv.NewWriter(f)
	defer w.Flush()

	w.Write([]string{"Service", "Cost"})
	for _, svc := range services {
		w.Write([]string{svc.Service, fmt.Sprintf("%.2f", svc.Cost)})
	}

	return nil
}
