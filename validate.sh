#!/bin/bash
# Validation script for execsummary package using real AWS data

set -e

ACCOUNT_ALIAS="${1:-}"
MONTHS="${2:-3}"
OUTPUT_DIR="validation_output"

if [ -z "$ACCOUNT_ALIAS" ]; then
  echo "Usage: $0 <account-alias> [months]"
  echo ""
  echo "Available accounts:"
  ./bin/finops account list 2>/dev/null | head -20 || echo "  (run: finops account list)"
  exit 1
fi

echo "========================================="
echo "Executive Summary Validation"
echo "========================================="
echo ""
echo "Account: $ACCOUNT_ALIAS"
echo "Months: $MONTHS"
echo "Output: $OUTPUT_DIR"
echo ""

# Create output directory
mkdir -p "$OUTPUT_DIR"

# Step 1: Fetch cost data using finops CLI
echo "Step 1: Fetching AWS cost data..."

# Fetch cost data split by service
echo "  Fetching service breakdown..."
./bin/finops cost get \
  --account-alias "$ACCOUNT_ALIAS" \
  --months "$MONTHS" \
  --split-by service \
  --format json > "$OUTPUT_DIR/cost_by_service.json"

echo "  ✓ Cost by service: $OUTPUT_DIR/cost_by_service.json"

# Fetch cost data split by account
echo "  Fetching account breakdown..."
./bin/finops cost get \
  --account-alias "$ACCOUNT_ALIAS" \
  --months "$MONTHS" \
  --split-by account \
  --format json > "$OUTPUT_DIR/cost_by_account.json" 2>/dev/null || echo "  (not a payer account)"

echo ""
echo "✅ Real AWS data fetched successfully!"
echo ""
echo "View data:"
echo "  cat $OUTPUT_DIR/cost_by_service.json | jq ."
echo "  cat $OUTPUT_DIR/cost_by_account.json | jq ."
echo ""
