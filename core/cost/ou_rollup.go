// ou_rollup.go aggregates linked-account cost breakdown rows into OU buckets / trees.
package cost

import (
	"sort"
	"strings"
)

const unknownOUBucketID = "unknown"

// OUHierarchyNode is one node in an OU tree under a selection root (DFS pre-order).
type OUHierarchyNode struct {
	ID       string
	Name     string
	ParentID string
	Depth    int
}

// RollupBreakdownByOU sums account-level breakdown rows into OU buckets (flat).
// Accounts missing from buckets are rolled into an "(unknown)" bucket.
func RollupBreakdownByOU(breakdown []CostBreakdownItem, buckets map[string]OUBucket) []CostBreakdownItem {
	if len(breakdown) == 0 {
		return nil
	}

	type agg struct {
		bucket OUBucket
		amount float64
	}
	byOU := make(map[string]*agg)

	for _, item := range breakdown {
		accountID := strings.TrimSpace(item.Account)
		bucket, ok := buckets[accountID]
		if !ok || strings.TrimSpace(bucket.ID) == "" {
			bucket = OUBucket{ID: unknownOUBucketID, Name: "(unknown)"}
		}
		key := strings.TrimSpace(bucket.ID)
		if existing, found := byOU[key]; found {
			existing.amount += item.Amount
			continue
		}
		byOU[key] = &agg{bucket: bucket, amount: item.Amount}
	}

	out := make([]CostBreakdownItem, 0, len(byOU))
	for _, a := range byOU {
		if a.amount == 0 {
			continue
		}
		name := strings.TrimSpace(a.bucket.Name)
		if name == "" {
			name = a.bucket.ID
		}
		out = append(out, CostBreakdownItem{
			OUID:   a.bucket.ID,
			OUName: name,
			Amount: a.amount,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Amount > out[j].Amount
	})
	return out
}

// RollupBreakdownByOUTree builds a DFS tree of OU costs.
// Each account is attributed to its immediate parent OU (parents map).
// Each node Amount is the subtree total (direct accounts + nested OUs).
// Sibling OUs are ordered by subtree amount descending; zero-subtree nodes are omitted.
func RollupBreakdownByOUTree(
	breakdown []CostBreakdownItem,
	parents map[string]OUBucket,
	hierarchy []OUHierarchyNode,
) []CostBreakdownItem {
	if len(breakdown) == 0 {
		return nil
	}

	direct := make(map[string]float64)
	var unknownAmount float64
	for _, item := range breakdown {
		accountID := strings.TrimSpace(item.Account)
		bucket, ok := parents[accountID]
		if !ok || strings.TrimSpace(bucket.ID) == "" {
			unknownAmount += item.Amount
			continue
		}
		direct[strings.TrimSpace(bucket.ID)] += item.Amount
	}

	byID := make(map[string]OUHierarchyNode, len(hierarchy))
	children := make(map[string][]string, len(hierarchy))
	var roots []string
	for _, n := range hierarchy {
		byID[n.ID] = n
		pid := strings.TrimSpace(n.ParentID)
		if pid == "" {
			roots = append(roots, n.ID)
			continue
		}
		children[pid] = append(children[pid], n.ID)
	}

	subtree := make(map[string]float64, len(hierarchy))
	var compute func(id string) float64
	compute = func(id string) float64 {
		if v, ok := subtree[id]; ok {
			return v
		}
		sum := direct[id]
		for _, childID := range children[id] {
			sum += compute(childID)
		}
		subtree[id] = sum
		return sum
	}
	for _, n := range hierarchy {
		compute(n.ID)
	}

	sortChildrenDesc := func(ids []string) {
		sort.SliceStable(ids, func(i, j int) bool {
			return subtree[ids[i]] > subtree[ids[j]]
		})
	}
	for pid, ids := range children {
		sortChildrenDesc(ids)
		children[pid] = ids
	}
	sortChildrenDesc(roots)

	out := make([]CostBreakdownItem, 0, len(hierarchy)+1)
	var emit func(id string)
	emit = func(id string) {
		amt := subtree[id]
		if amt == 0 {
			return
		}
		n := byID[id]
		name := strings.TrimSpace(n.Name)
		if name == "" {
			name = n.ID
		}
		out = append(out, CostBreakdownItem{
			OUID:    n.ID,
			OUName:  name,
			OUDepth: n.Depth,
			Amount:  amt,
		})
		for _, childID := range children[id] {
			emit(childID)
		}
	}
	for _, id := range roots {
		emit(id)
	}
	if unknownAmount != 0 {
		out = append(out, CostBreakdownItem{
			OUID:   unknownOUBucketID,
			OUName: "(unknown)",
			Amount: unknownAmount,
		})
	}
	return out
}

// rollupOUBreakdown applies flat or tree OU rollup using AWS fetch options.
func rollupOUBreakdown(breakdown []CostBreakdownItem, opts *AWSFetchOptions) []CostBreakdownItem {
	var buckets map[string]OUBucket
	var hierarchy []OUHierarchyNode
	if opts != nil {
		buckets = opts.AccountOUBuckets
		hierarchy = opts.OUHierarchy
	}
	if len(hierarchy) > 0 {
		return RollupBreakdownByOUTree(breakdown, buckets, hierarchy)
	}
	return RollupBreakdownByOU(breakdown, buckets)
}
