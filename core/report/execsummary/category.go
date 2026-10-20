package execsummary

import "sort"

// EnrichCostRecords joins raw AWS cost rows with the mapping CSV's Service
// Name and Account Name, falling back to UnmappedServiceName /
// the raw account ID respectively for any AWS account missing from the
// mapping (spec 3.4, 6.7).
func EnrichCostRecords(records []CostRecord, mapping AccountTaxonomyMap) []EnrichedCostRecord {
	out := make([]EnrichedCostRecord, len(records))
	for i, r := range records {
		out[i] = EnrichedCostRecord{
			CostRecord:  r,
			ServiceName: mapping.ServiceName(r.AccountID),
			AccountName: mapping.AccountName(r.AccountID),
		}
	}
	return out
}

// CategoryMonthly implements spec 6.7: group enriched cost rows by (Service
// Name, month) and sum cost. Output is sorted by month then Service Name
// for deterministic rendering.
func CategoryMonthly(enriched []EnrichedCostRecord) []CategoryMonthRecord {
	type key struct{ service, month string }
	totals := make(map[key]float64)
	seen := make(map[key]bool)
	var order []key

	for _, r := range enriched {
		k := key{r.ServiceName, r.Month}
		if !seen[k] {
			seen[k] = true
			order = append(order, k)
		}
		totals[k] += r.Cost
	}

	out := make([]CategoryMonthRecord, 0, len(order))
	for _, k := range order {
		out = append(out, CategoryMonthRecord{
			ServiceName: k.service,
			Month:       k.month,
			Cost:        totals[k],
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Month != out[j].Month {
			return out[i].Month < out[j].Month
		}
		return out[i].ServiceName < out[j].ServiceName
	})
	return out
}
