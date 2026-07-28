// merge.go combines per-account CostResult values into a single summary with merged breakdown rows.
package cost

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

// MergeResults combines per-account cost results into one summary.
func MergeResults(results []CostResult) (CostResult, error) {
	if len(results) == 0 {
		return CostResult{}, errors.New("no results to merge")
	}
	if len(results) == 1 {
		return results[0], nil
	}

	first := results[0]
	out := CostResult{
		Provider:  first.Provider,
		Metric:    first.Metric,
		SplitBy:   first.SplitBy,
		StartDate: first.StartDate,
		EndDate:   first.EndDate,
		Currency:  first.Currency,
	}

	var (
		names []string
		ids   []string
	)
	byKey := make(map[string]float64)
	accountNames := make(map[string]string)
	ouDepths := make(map[string]int)
	ouOrder := make([]string, 0)
	ouSeen := make(map[string]struct{})
	preserveOUOrder := out.SplitBy == SplitByOU

	for _, r := range results {
		if r.Provider != out.Provider {
			return CostResult{}, fmt.Errorf("cannot merge different providers")
		}
		if r.Currency != out.Currency {
			return CostResult{}, fmt.Errorf("cannot merge accounts with different currencies (%s vs %s)",
				out.Currency, r.Currency)
		}
		if r.StartDate != out.StartDate || r.EndDate != out.EndDate {
			return CostResult{}, fmt.Errorf("cannot merge accounts with different periods")
		}
		if r.SplitBy != out.SplitBy {
			return CostResult{}, fmt.Errorf("cannot merge accounts with different split-by settings")
		}

		names = append(names, r.AccountName)
		ids = append(ids, r.AccountID)
		if r.Linked {
			out.Linked = true
		}
		out.Amount += r.Amount

		for _, item := range r.Breakdown {
			key := item.Label(out.SplitBy)
			byKey[key] += item.Amount
			if out.SplitBy == SplitByAccount && item.AccountName != "" {
				accountNames[key] = item.AccountName
			}
			if out.SplitBy == SplitByOU {
				if item.OUName != "" {
					accountNames[key] = item.OUName
				}
				if _, ok := ouDepths[key]; !ok {
					ouDepths[key] = item.OUDepth
				}
				if preserveOUOrder {
					if _, seen := ouSeen[key]; !seen {
						ouSeen[key] = struct{}{}
						ouOrder = append(ouOrder, key)
					}
				}
			}
		}
	}

	out.AccountName = strings.Join(names, ", ")
	out.AccountID = strings.Join(ids, ", ")

	if len(byKey) > 0 {
		out.Breakdown = make([]CostBreakdownItem, 0, len(byKey))
		for key, amt := range byKey {
			if amt == 0 {
				continue
			}
			item := CostBreakdownItem{Amount: amt}
			switch out.SplitBy {
			case SplitByAccount:
				item.Account = key
				item.AccountName = accountNames[key]
			case SplitByOU:
				item.OUID = key
				item.OUName = accountNames[key]
				if item.OUName == "" {
					item.OUName = key
				}
				item.OUDepth = ouDepths[key]
			default:
				item.Service = key
			}
			out.Breakdown = append(out.Breakdown, item)
		}
		if preserveOUOrder && len(ouOrder) > 0 {
			byID := make(map[string]CostBreakdownItem, len(out.Breakdown))
			for _, item := range out.Breakdown {
				byID[item.OUID] = item
			}
			ordered := make([]CostBreakdownItem, 0, len(ouOrder))
			for _, key := range ouOrder {
				if item, ok := byID[key]; ok {
					ordered = append(ordered, item)
				}
			}
			out.Breakdown = ordered
		} else {
			sort.Slice(out.Breakdown, func(i, j int) bool {
				return out.Breakdown[i].Amount > out.Breakdown[j].Amount
			})
		}
	}

	return out, nil
}

// MergeDaily combines per-account daily series by summing amounts per date.
func MergeDaily(series [][]DailyCostItem) []DailyCostItem {
	if len(series) == 0 {
		return nil
	}
	if len(series) == 1 {
		return series[0]
	}

	byDate := make(map[string]float64)
	for _, s := range series {
		for _, item := range s {
			byDate[item.Date] += item.Amount
		}
	}

	out := make([]DailyCostItem, 0, len(byDate))
	for date, amt := range byDate {
		if amt == 0 {
			continue
		}
		out = append(out, DailyCostItem{Date: date, Amount: amt})
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Date < out[j].Date
	})
	return out
}
