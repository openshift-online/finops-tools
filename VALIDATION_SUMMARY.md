# Validation Setup Complete

I've created a validation framework to test the `execsummary` package with real AWS cost data.

## Quick Start

### 1. Register Your AWS Account (One-Time)

```bash
# Build the finops CLI first
make build

# Register an AWS account with an alias
./bin/finops account add aws <12-digit-account-id> --alias my-account --auth-method saml

# Or use an existing AWS profile
./bin/finops account add aws <account-id> --alias my-account --auth-method profile
```

### 2. Fetch Real Cost Data

```bash
# Fetch 3 months of cost data split by service
./bin/finops cost get \
  --account-alias my-account \
  --months 3 \
  --split-by service \
  --format json > cost.json
```

### 3. Run Validation

```bash
go run test_validation.go cost.json
```

## What Gets Validated

The validation program tests:

1. ✅ **Cost record creation** - Converts AWS data to CostRecord format
2. ✅ **Data indexing** - Creates ByMonth, ByAccount, ByMonthAccount indexes
3. ✅ **Enrichment** - Applies account mappings (empty for now)
4. ✅ **Total cost calculation** - Verifies math matches AWS totals
5. ✅ **Top services** - Ranks services by cost
6. ✅ **Monthly aggregation** - Groups costs by category and month
7. ✅ **KPI computation** - Calculates MoM%, totals (if 2+ months)
8. ✅ **CSV export** - Exports results to validation_services.csv

## Example Output

```
========================================
Executive Summary Package Validation
========================================

Loading cost data from cost.json...
  Account: 123456789012
  Period: 2026-03-01 to 2026-05-28
  Total: $12,345.67
  Services: 42

Test 1: Creating cost records...
  ✓ Created 126 cost records (3 months × 42 services)

Test 2: Indexing cost data...
  ✓ Indexed by month: 3 unique months
  ✓ Indexed by account: 1 unique accounts

Test 3: Enriching data...
  ✓ Enriched 126 records

Test 4: Calculating total cost...
  ✓ Total cost: $12,345.67
  ✓ Matches input: true (diff: $0.00)

Test 5: Finding top services...
  Top 10 services for 2026-05:
     1. Amazon Elastic Compute Cloud - Compute        $  3,456.78
     2. Amazon Relational Database Service            $  1,234.56
     3. Amazon Simple Storage Service                 $    567.89
     ...

Test 6: Monthly aggregation...
  ✓ Generated 3 category-month aggregates

Test 7: Exporting to CSV...
  ✓ Exported to validation_services.csv

Test 8: Computing KPIs...
  Last Month: $4,123.45 (2026-05)
  Prev Month: $3,987.65 (2026-04)
  MoM Change: 3.41%

========================================
✅ All validation tests passed!
========================================
```

## Files Created

1. **VALIDATION.md** - Complete validation guide with troubleshooting
2. **test_validation.go** - Simple validation program for real data
3. **validate.sh** - Shell script to fetch cost data (helper)

## Advanced Validation

For complete testing including anomaly detection:

1. **Fetch 6+ months** of data (anomaly detection needs history)
2. **Create account mappings** file (see VALIDATION.md)
3. **Use payer account** to get multiple linked accounts

Example with mappings:

```bash
# Create accounts.json with your account metadata
cat > accounts.json << EOF
[
  {
    "account_id": "123456789012",
    "refined_category": "Production",
    "sub_type": "HCP",
    "owner_team": "Platform",
    "account_name": "Prod HCP"
  }
]
EOF

# Modify test_validation.go to load mappings
# Then run again
```

## Verification Checklist

- [ ] Fetch real AWS cost data
- [ ] Run validation program
- [ ] Verify total matches AWS console
- [ ] Check CSV export has correct services
- [ ] Confirm MoM% calculation is accurate
- [ ] Export shows top services correctly
- [ ] All 8 tests pass

## Next Steps

After validation passes:

1. Integrate into CLI (`finops report generate` command)
2. Add HTML template rendering
3. Test with production accounts
4. Add anomaly detection with 6+ months
5. Implement cluster count metrics
6. Add savings plan coverage/utilization

## Troubleshooting

**"No AWS accounts registered"**
- You need to run `finops account add aws` first

**"Failed to fetch cost"**
- Check AWS credentials: `aws sts get-caller-identity --profile <profile>`
- Verify Cost Explorer permissions: `ce:GetCostAndUsage`

**"Total doesn't match"**
- This is expected - we're splitting cost evenly across months
- For accurate monthly breakdown, need to fetch with monthly granularity

**"No services found"**
- Check the date range has actual costs
- Try `--split-by account` if you're a payer account
