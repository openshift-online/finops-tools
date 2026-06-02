# Executive Summary Package Validation

This document describes how to validate the `execsummary` package with real AWS cost data.

## Prerequisites

1. **Build the finops CLI**:
   ```bash
   make build
   ```

2. **Register your AWS account** (one-time setup):
   ```bash
   # For a payer/management account:
   ./bin/finops account add aws <12-digit-account-id> --alias my-payer

   # For authentication, you can use:
   # - SAML (default): built-in Red Hat SAML login
   # - Profile: use existing AWS profile
   ./bin/finops account add aws <account-id> --alias my-account --auth-method profile
   ```

   Or use SAML login:
   ```bash
   rh-aws-saml-login  # Login first
   ./bin/finops account add aws <account-id> --alias my-account --auth-method saml
   ```

## Quick Validation

### Step 1: Fetch Real Cost Data

Fetch 3 months of cost data split by service:

```bash
./bin/finops cost get \
  --account-alias my-account \
  --months 3 \
  --split-by service \
  --format json > cost_by_service.json
```

Fetch cost data split by linked account (if payer):

```bash
./bin/finops cost get \
  --account-alias my-account \
  --months 3 \
  --split-by account \
  --format json > cost_by_account.json
```

### Step 2: Verify the Data

```bash
# View service breakdown
cat cost_by_service.json | jq .

# Check total cost
cat cost_by_service.json | jq '.amount'

# List services
cat cost_by_service.json | jq '.breakdown[] | {service, amount}'
```

### Step 3: Test Package Functions

Create a simple Go program to test the execsummary functions:

```go
package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/openshift-online/finops-tools/core/report/execsummary"
)

type CostResult struct {
	Provider  string    `json:"provider"`
	AccountID string    `json:"account_id"`
	Amount    float64   `json:"amount"`
	Breakdown []struct {
		Service string  `json:"service"`
		Amount  float64 `json:"amount"`
	} `json:"breakdown"`
	StartDate string `json:"start_date"`
	EndDate   string `json:"end_date"`
}

func main() {
	// Load the JSON data
	data, err := os.ReadFile("cost_by_service.json")
	if err != nil {
		log.Fatal(err)
	}

	var result CostResult
	if err := json.Unmarshal(data, &result); err != nil {
		log.Fatal(err)
	}

	fmt.Printf("Loaded cost data for account %s\n", result.AccountID)
	fmt.Printf("Total: $%.2f (%s to %s)\n", result.Amount, result.StartDate, result.EndDate)
	fmt.Printf("Services: %d\n\n", len(result.Breakdown))

	// Test 1: Create cost records
	var records []execsummary.CostRecord
	for _, svc := range result.Breakdown {
		records = append(records, execsummary.CostRecord{
			Month:     "2026-05", // You'd parse this from the date range
			AccountID: result.AccountID,
			Cost:      svc.Amount,
			Service:   svc.Service,
		})
	}

	// Test 2: Create CostData with indexing
	costData := &execsummary.CostData{
		Records:        records,
		ByMonth:        make(map[string][]execsummary.CostRecord),
		ByAccount:      make(map[string][]execsummary.CostRecord),
		ByMonthAccount: make(map[string]map[string]float64),
	}

	for _, rec := range records {
		costData.ByMonth[rec.Month] = append(costData.ByMonth[rec.Month], rec)
		costData.ByAccount[rec.AccountID] = append(costData.ByAccount[rec.AccountID], rec)
		
		if costData.ByMonthAccount[rec.Month] == nil {
			costData.ByMonthAccount[rec.Month] = make(map[string]float64)
		}
		costData.ByMonthAccount[rec.Month][rec.AccountID] += rec.Cost
	}

	fmt.Printf("✓ Indexed %d cost records\n", len(costData.Records))
	fmt.Printf("✓ %d unique months\n", len(costData.ByMonth))
	fmt.Printf("✓ %d unique accounts\n\n", len(costData.ByAccount))

	// Test 3: Enrich with empty mappings (no account mapping file)
	enriched := execsummary.EnrichWithMapping(costData, []execsummary.AccountMapping{})
	fmt.Printf("✓ Enriched %d records\n", len(enriched))

	// Test 4: Calculate total cost
	total := execsummary.TotalCost(enriched)
	fmt.Printf("✓ Total cost: $%.2f\n\n", total)

	// Test 5: Top services
	topServices := execsummary.ServiceCostsForAccount(enriched, result.AccountID, "2026-05", 10)
	fmt.Printf("Top 10 services:\n")
	for i, svc := range topServices {
		fmt.Printf("  %2d. %-30s $%10.2f\n", i+1, svc.Service, svc.Cost)
	}

	fmt.Printf("\n✅ All basic functions working!\n")
}
```

Save as `test_validation.go` and run:

```bash
go run test_validation.go
```

## Full Integration Test

To test the complete pipeline including anomaly detection and KPIs, you need:

1. **Multiple months of data** (at least 3-6 months)
2. **Account mapping file** (optional but recommended)
3. **Multiple accounts** (for anomaly detection across accounts)

Example account mapping file `accounts.json`:

```json
[
  {
    "account_id": "123456789012",
    "refined_category": "Production",
    "sub_type": "HCP",
    "owner_team": "Platform Team",
    "account_name": "Production HCP"
  }
]
```

## Expected Output

When running with real data, you should see:

1. **Raw cost data**: List of services and costs
2. **Enriched data**: Cost records with account metadata
3. **Monthly aggregates**: Costs grouped by month and category
4. **Anomalies**: Accounts with unusual cost patterns (requires 3+ months)
5. **Growing accounts**: Top accounts by month-over-month growth
6. **KPIs**: Total costs, MoM percentage, anomaly counts

## Validation Checklist

- [ ] Fetch real cost data from AWS
- [ ] Load and index cost records
- [ ] Enrich with account mappings
- [ ] Calculate monthly aggregates
- [ ] Detect anomalies (3+ months required)
- [ ] Find top growing accounts
- [ ] Compute KPIs
- [ ] Export to CSV
- [ ] Verify calculations against AWS console

## Troubleshooting

**"No AWS accounts registered"**:
- Run `finops account add aws` to register your account first

**"Permission denied"**:
- Ensure your AWS credentials have Cost Explorer permissions
- Check IAM role: `ce:GetCostAndUsage`

**"No anomalies detected"**:
- Need at least 3 months of data
- Accounts must have sufficient variance (>$10 stddev)

**"Empty breakdown"**:
- Check date range is valid
- Verify account has actual costs in that period
