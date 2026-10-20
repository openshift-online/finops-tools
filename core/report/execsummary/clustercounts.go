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

// ClusterGrowthInput mirrors hcphierarchy.ClusterGrowthRow so this package
// stays dependency-free (see region.go's ClusterRegionInput for the same
// pattern): new (not cumulative) worker/standalone clusters created in one
// month, for one product type.
type ClusterGrowthInput struct {
	Month       string // YYYY-MM
	ProductType string
	NewClusters int
}

// clusterGrowthProductOrder is the spec 7.7 canonical product display
// order; any product type not listed here sorts alphabetically after it.
var clusterGrowthProductOrder = []string{"ROSA HCP", "ROSA Classic", "OSD", "OSD Trial"}

// ClusterGrowthProductPoint is one product type's cumulative cluster count
// and net-new delta for one month (spec section 7.7).
type ClusterGrowthProductPoint struct {
	ProductType string
	Month       string
	Cumulative  int
	NewClusters int
}

// SortProductTypes orders product types per spec 7.7's canonical display
// order (ROSA HCP, ROSA Classic, OSD, OSD Trial), appending any other
// product type alphabetically afterward.
func SortProductTypes(products []string) []string {
	rank := make(map[string]int, len(clusterGrowthProductOrder))
	for i, p := range clusterGrowthProductOrder {
		rank[p] = i
	}
	out := append([]string(nil), products...)
	sort.Slice(out, func(i, j int) bool {
		ri, iKnown := rank[out[i]]
		rj, jKnown := rank[out[j]]
		switch {
		case iKnown && jKnown:
			return ri < rj
		case iKnown:
			return true
		case jKnown:
			return false
		default:
			return out[i] < out[j]
		}
	})
	return out
}

// BuildClusterGrowthPoints implements spec section 7.7: cumulative cluster
// count and net-new delta per (product type, month) for every month in
// `months`, using each product type's full all-time history (see
// CumulativeCounts) so the window's first month's cumulative total is
// correct. rows should include the bucket's full all-time history, not
// just the reporting window.
func BuildClusterGrowthPoints(rows []ClusterGrowthInput, months []string) []ClusterGrowthProductPoint {
	byProduct := make(map[string]map[string]int)
	productsSeen := make(map[string]bool)
	for _, r := range rows {
		if byProduct[r.ProductType] == nil {
			byProduct[r.ProductType] = make(map[string]int)
		}
		byProduct[r.ProductType][r.Month] += r.NewClusters
		productsSeen[r.ProductType] = true
	}

	products := make([]string, 0, len(productsSeen))
	for p := range productsSeen {
		products = append(products, p)
	}
	products = SortProductTypes(products)

	var out []ClusterGrowthProductPoint
	for _, p := range products {
		newByMonth := byProduct[p]
		cumulative := CumulativeCounts(newByMonth, months)
		for _, m := range months {
			out = append(out, ClusterGrowthProductPoint{
				ProductType: p,
				Month:       m,
				Cumulative:  cumulative[m],
				NewClusters: newByMonth[m],
			})
		}
	}
	return out
}

// ClusterGrowthTabReport is the complete computed content for the
// "Cluster Growth" tab (spec section 7.7).
type ClusterGrowthTabReport struct {
	Months       []string // ascending YYYY-MM keys
	ProductTypes []string // present product types, in spec 7.7's canonical display order
	Points       []ClusterGrowthProductPoint
}
