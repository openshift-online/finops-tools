package execsummary

import (
	"math"
	"sort"
	"time"
)

// Spec 6.9 constants: z-score threshold, minimum standard deviation (to
// avoid false positives on near-flat/low-cost accounts), and the maximum
// number of anomalies returned.
const (
	anomalyZThreshold = 2.0
	anomalyMinSigma   = 10.0
	anomalyTopN       = 15
)

// StatisticalAnomalies implements spec 6.9: for each account, compare its
// most recent month's cost against the mean/stddev of its cost in all prior
// months in the window. Requires at least 3 months of data overall and at
// least 2 prior months of data per account; skips accounts with stddev <
// $10 or |z| < 2.0. Returns at most the top 15 anomalies by |z-score|,
// descending.
func StatisticalAnomalies(enriched []EnrichedCostRecord, months []time.Time) []AnomalyRecord {
	if len(months) < 3 {
		return nil
	}
	lastMonth := MonthLabel(months[len(months)-1])

	byAccountMonth := map[string]map[string]float64{}
	meta := map[string]EnrichedCostRecord{}
	payerByAccount := map[string]string{}

	for _, r := range enriched {
		if byAccountMonth[r.AccountID] == nil {
			byAccountMonth[r.AccountID] = map[string]float64{}
		}
		byAccountMonth[r.AccountID][r.Month] += r.Cost
		if _, ok := meta[r.AccountID]; !ok {
			meta[r.AccountID] = r
		}
		if r.Payer != "" {
			payerByAccount[r.AccountID] = r.Payer
		}
	}

	accountIDs := make([]string, 0, len(byAccountMonth))
	for id := range byAccountMonth {
		accountIDs = append(accountIDs, id)
	}
	sort.Strings(accountIDs) // deterministic iteration order

	var anomalies []AnomalyRecord
	for _, accountID := range accountIDs {
		monthly := byAccountMonth[accountID]

		var prior []float64
		for month, cost := range monthly {
			if month < lastMonth {
				prior = append(prior, cost)
			}
		}
		if len(prior) < 2 {
			continue
		}

		mu := mean(prior)
		sigma := stddev(prior, mu)
		if sigma < anomalyMinSigma {
			continue
		}

		current, ok := monthly[lastMonth]
		if !ok {
			continue
		}

		z := (current - mu) / sigma
		if math.Abs(z) < anomalyZThreshold {
			continue
		}

		var pctChange float64
		if mu != 0 {
			pctChange = (current - mu) / mu * 100
		}
		direction := "spike"
		if z < 0 {
			direction = "drop"
		}

		m := meta[accountID]
		anomalies = append(anomalies, AnomalyRecord{
			AccountID:   accountID,
			AccountName: m.AccountName,
			ServiceName: m.ServiceName,
			Payer:       payerByAccount[accountID],
			Month:       lastMonth,
			CurrentCost: current,
			MeanCost:    mu,
			ZScore:      z,
			PctChange:   pctChange,
			Direction:   direction,
		})
	}

	sort.Slice(anomalies, func(i, j int) bool {
		return math.Abs(anomalies[i].ZScore) > math.Abs(anomalies[j].ZScore)
	})
	if len(anomalies) > anomalyTopN {
		anomalies = anomalies[:anomalyTopN]
	}
	return anomalies
}

// mean returns the arithmetic mean of values, or 0 for an empty slice.
func mean(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	var sum float64
	for _, v := range values {
		sum += v
	}
	return sum / float64(len(values))
}

// stddev returns the sample standard deviation (n-1 denominator) of values
// around m, or 0 if fewer than 2 values are given.
func stddev(values []float64, m float64) float64 {
	if len(values) < 2 {
		return 0
	}
	var variance float64
	for _, v := range values {
		d := v - m
		variance += d * d
	}
	variance /= float64(len(values) - 1)
	return math.Sqrt(variance)
}
