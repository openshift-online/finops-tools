package execsummary

import "sort"

// SPMonthlyMetric is one payer's Savings Plans coverage or utilization
// percentage for one month -- the minimal shape needed to combine Savings
// Plans data across payers (spec 3.3). Mirrors
// core/report/savingsplans.MonthlyMetric field-for-field so callers can
// convert trivially.
type SPMonthlyMetric struct {
	Month      string
	Percentage float64
}

// SPMonthlySavings is one payer's Savings Plans net savings for one month.
// Mirrors core/report/savingsplans.MonthlySavings field-for-field.
type SPMonthlySavings struct {
	Month                  string
	NetSavings             float64
	OnDemandCostEquivalent float64
}

// CombinePercentageAcrossPayers averages a Savings Plans coverage or
// utilization percentage metric across payers, per month, by simple
// arithmetic mean (spec 3.3: "not spend-weighted"). A payer missing data
// for a given month is excluded from that month's average rather than
// treated as 0. Output is sorted by month.
func CombinePercentageAcrossPayers(perPayer [][]SPMonthlyMetric) []SPMonthlyMetric {
	sums := map[string]float64{}
	counts := map[string]int{}
	for _, metrics := range perPayer {
		for _, m := range metrics {
			sums[m.Month] += m.Percentage
			counts[m.Month]++
		}
	}

	months := make([]string, 0, len(sums))
	for m := range sums {
		months = append(months, m)
	}
	sort.Strings(months)

	out := make([]SPMonthlyMetric, 0, len(months))
	for _, m := range months {
		out = append(out, SPMonthlyMetric{Month: m, Percentage: sums[m] / float64(counts[m])})
	}
	return out
}

// CombineSavingsAcrossPayers sums Savings Plans net savings and on-demand-
// equivalent cost across payers, per month (spec 3.3). Output is sorted by
// month.
func CombineSavingsAcrossPayers(perPayer [][]SPMonthlySavings) []SPMonthlySavings {
	netSum := map[string]float64{}
	onDemandSum := map[string]float64{}
	seen := map[string]bool{}
	var months []string

	for _, savings := range perPayer {
		for _, s := range savings {
			if !seen[s.Month] {
				seen[s.Month] = true
				months = append(months, s.Month)
			}
			netSum[s.Month] += s.NetSavings
			onDemandSum[s.Month] += s.OnDemandCostEquivalent
		}
	}
	sort.Strings(months)

	out := make([]SPMonthlySavings, 0, len(months))
	for _, m := range months {
		out = append(out, SPMonthlySavings{
			Month:                  m,
			NetSavings:             netSum[m],
			OnDemandCostEquivalent: onDemandSum[m],
		})
	}
	return out
}

// TotalSavings sums NetSavings and OnDemandCostEquivalent across every
// month in savings -- the window-level totals consumed by
// PayerKPIInputs.TotalNetSavings / TotalOnDemandEquiv (spec 6.10).
func TotalSavings(savings []SPMonthlySavings) (netSavings, onDemandEquiv float64) {
	for _, s := range savings {
		netSavings += s.NetSavings
		onDemandEquiv += s.OnDemandCostEquivalent
	}
	return netSavings, onDemandEquiv
}

// AveragePercentage returns the simple arithmetic mean of Percentage across
// metrics, or nil if metrics is empty -- the window-level average consumed
// by PayerKPIInputs.SPCoverageAvg / SPUtilizationAvg.
func AveragePercentage(metrics []SPMonthlyMetric) *float64 {
	if len(metrics) == 0 {
		return nil
	}
	var sum float64
	for _, m := range metrics {
		sum += m.Percentage
	}
	avg := sum / float64(len(metrics))
	return &avg
}
