package execsummary

import "sort"

// CumulativeCounts converts raw new-entries-per-month (e.g. one bucket's
// hcphierarchy.ClusterCountRow.NewClusters or
// hcphierarchy.ClusterGrowthRow.NewClusters, grouped by environment or
// product type respectively) into a cumulative running total for every
// month in `months` (spec section 5 steps 3/4: "compute the cumulative
// running total ... in application code").
//
// `months` must be sorted ascending (e.g. MonthLabel applied to
// TimeWindow.Months). `newByMonth` should contain the bucket's full
// all-time history, not just the reporting window -- entries created
// before `months[0]` still count toward the window's first month's
// cumulative total; without full history the window's totals would be
// understated.
func CumulativeCounts(newByMonth map[string]int, months []string) map[string]int {
	allMonths := make([]string, 0, len(newByMonth))
	for m := range newByMonth {
		allMonths = append(allMonths, m)
	}
	sort.Strings(allMonths)

	out := make(map[string]int, len(months))
	running := 0
	histIdx := 0
	for _, wm := range months {
		for histIdx < len(allMonths) && allMonths[histIdx] <= wm {
			running += newByMonth[allMonths[histIdx]]
			histIdx++
		}
		out[wm] = running
	}
	return out
}

// ClusterMoMDelta implements spec 6.11: cluster_count[month] -
// cluster_count[month-1]. cumulative should be month -> cumulative-count,
// e.g. the output of CumulativeCounts. Returns nil if either month is
// missing from cumulative (no prior-month snapshot exists).
func ClusterMoMDelta(cumulative map[string]int, month, prevMonth string) *int {
	cur, curOK := cumulative[month]
	prev, prevOK := cumulative[prevMonth]
	if !curOK || !prevOK {
		return nil
	}
	delta := cur - prev
	return &delta
}
