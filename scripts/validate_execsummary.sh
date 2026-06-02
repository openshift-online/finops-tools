#!/bin/bash
# Validation script for execsummary package using real AWS data

set -e

PROFILE="${1:-default}"
MONTHS="${2:-6}"
OUTPUT_DIR="validation_output"

echo "========================================="
echo "Executive Summary Validation"
echo "========================================="
echo ""
echo "Profile: $PROFILE"
echo "Months: $MONTHS"
echo "Output: $OUTPUT_DIR"
echo ""

# Create output directory
mkdir -p "$OUTPUT_DIR"

# Step 1: Use finops CLI to export cost data
echo "Step 1: Fetching AWS cost data via finops CLI..."

# Calculate date range
START_DATE=$(date -v-${MONTHS}m -u +"%Y-%m-01")
END_DATE=$(date -u +"%Y-%m-%d")

echo "  Date range: $START_DATE to $END_DATE"

# Fetch cost data split by service
echo "  Fetching service breakdown..."
./bin/finops cost get \
  --profile "$PROFILE" \
  --start "$START_DATE" \
  --end "$END_DATE" \
  --split-by service \
  --output json > "$OUTPUT_DIR/cost_by_service.json"

# Fetch cost data split by account (if management account)
echo "  Fetching account breakdown..."
./bin/finops cost get \
  --profile "$PROFILE" \
  --start "$START_DATE" \
  --end "$END_DATE" \
  --split-by account \
  --output json > "$OUTPUT_DIR/cost_by_account.json" 2>/dev/null || echo "  (skipped - not a payer account)"

echo "  ✓ Cost data exported to $OUTPUT_DIR/"
echo ""
echo "Step 2: Now run the validation program to process this data:"
echo "  go run core/report/execsummary/cmd/validate/main.go \\"
echo "    -data $OUTPUT_DIR \\"
echo "    -accounts accounts.json"
echo ""
