// account_tag_rollup.go aggregates linked-account cost breakdown rows by Organizations account tag value.
package cost

import (
	"sort"
	"strings"
)

const untaggedAccountTagValue = "(untagged)"

// RollupBreakdownByAccountTag sums account-level breakdown rows into tag-value buckets.
// Accounts missing from values (or with the tag key absent) roll into "(untagged)".
func RollupBreakdownByAccountTag(breakdown []CostBreakdownItem, values map[string]string) []CostBreakdownItem {
	if len(breakdown) == 0 {
		return nil
	}

	byValue := make(map[string]float64)
	for _, item := range breakdown {
		accountID := strings.TrimSpace(item.Account)
		tagValue, ok := values[accountID]
		if !ok {
			tagValue = untaggedAccountTagValue
		}
		byValue[tagValue] += item.Amount
	}

	out := make([]CostBreakdownItem, 0, len(byValue))
	for tagValue, amount := range byValue {
		if amount == 0 {
			continue
		}
		item := CostBreakdownItem{Amount: amount}
		item.SetTagValue(tagValue)
		out = append(out, item)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Amount > out[j].Amount
	})
	return out
}

func rollupAccountTagBreakdown(breakdown []CostBreakdownItem, opts *AWSFetchOptions) []CostBreakdownItem {
	var values map[string]string
	if opts != nil {
		values = opts.AccountTagValues
	}
	return RollupBreakdownByAccountTag(breakdown, values)
}
