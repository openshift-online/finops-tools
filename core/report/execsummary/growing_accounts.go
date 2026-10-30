package execsummary

import (
	"sort"
	"time"
)

// TopGrowingAccounts implements spec 6.8: rank accounts by month-over-month
// cost delta between the window's two most recent months (descending) and
// return the topN, each enriched with its top topSvcN services by cost in
// the last month. Returns nil if months has fewer than 2 entries.
func TopGrowingAccounts(enriched []EnrichedCostRecord, months []time.Time, topN, topSvcN int) []GrowingAccountRecord {
	if len(months) < 2 {
		return nil
	}
	lastMonth := MonthLabel(months[len(months)-1])
	prevMonth := MonthLabel(months[len(months)-2])

	lastByAccount := map[string]float64{}
	prevByAccount := map[string]float64{}
	meta := map[string]EnrichedCostRecord{} // first-seen record per account

	for _, r := range enriched {
		if _, ok := meta[r.AccountID]; !ok {
			meta[r.AccountID] = r
		}
		switch r.Month {
		case lastMonth:
			lastByAccount[r.AccountID] += r.Cost
		case prevMonth:
			prevByAccount[r.AccountID] += r.Cost
		}
	}

	accounts := make(map[string]bool)
	for id := range lastByAccount {
		accounts[id] = true
	}
	for id := range prevByAccount {
		accounts[id] = true
	}
	if len(accounts) == 0 {
		return nil
	}

	type deltaPair struct {
		accountID string
		delta     float64
	}
	pairs := make([]deltaPair, 0, len(accounts))
	for id := range accounts {
		pairs = append(pairs, deltaPair{id, lastByAccount[id] - prevByAccount[id]})
	}
	sort.Slice(pairs, func(i, j int) bool {
		if pairs[i].delta != pairs[j].delta {
			return pairs[i].delta > pairs[j].delta
		}
		return pairs[i].accountID < pairs[j].accountID // deterministic tiebreak
	})
	if len(pairs) > topN {
		pairs = pairs[:topN]
	}

	out := make([]GrowingAccountRecord, 0, len(pairs))
	for _, p := range pairs {
		m := meta[p.accountID]
		out = append(out, GrowingAccountRecord{
			AccountID:     p.accountID,
			AccountName:   m.AccountName,
			ServiceName:   m.ServiceName,
			LastMonthCost: lastByAccount[p.accountID],
			PrevMonthCost: prevByAccount[p.accountID],
			Delta:         p.delta,
			TopServices:   TopServicesForAccount(enriched, p.accountID, lastMonth, topSvcN),
		})
	}
	return out
}

// TopServicesForAccount returns the topN AWS services with the highest cost
// for accountID in month, sorted by cost descending (spec 6.8's
// top_3_services).
func TopServicesForAccount(enriched []EnrichedCostRecord, accountID, month string, topN int) []ServiceCostPair {
	totals := map[string]float64{}
	for _, r := range enriched {
		if r.AccountID != accountID || r.Month != month {
			continue
		}
		totals[r.Service] += r.Cost
	}
	pairs := make([]ServiceCostPair, 0, len(totals))
	for svc, cost := range totals {
		pairs = append(pairs, ServiceCostPair{Service: svc, Cost: cost})
	}
	sort.Slice(pairs, func(i, j int) bool {
		if pairs[i].Cost != pairs[j].Cost {
			return pairs[i].Cost > pairs[j].Cost
		}
		return pairs[i].Service < pairs[j].Service // deterministic tiebreak
	})
	if len(pairs) > topN {
		pairs = pairs[:topN]
	}
	return pairs
}
