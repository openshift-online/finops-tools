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

	// parents may reference an OU missing from hierarchy (caller inconsistency).
	// Fold those amounts into unknown so cost is never silently dropped.
	for ouID, amount := range direct {
		if _, ok := byID[ouID]; !ok {
			unknownAmount += amount
			delete(direct, ouID)
		}
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

// FormatAccountOUPaths builds "Root / Parent / OU" labels for each account from
// immediate-parent buckets and the OU tree under the selection root.
func FormatAccountOUPaths(parents map[string]OUBucket, hierarchy []OUHierarchyNode) map[string]string {
	byID := make(map[string]OUHierarchyNode, len(hierarchy))
	for _, n := range hierarchy {
		id := strings.TrimSpace(n.ID)
		if id == "" {
			continue
		}
		byID[id] = n
	}
	out := make(map[string]string, len(parents))
	for accountID, bucket := range parents {
		path := formatOUPath(bucket, byID)
		if path == "" {
			continue
		}
		out[accountID] = path
	}
	return out
}

func formatOUPath(bucket OUBucket, byID map[string]OUHierarchyNode) string {
	id := strings.TrimSpace(bucket.ID)
	if id == "" {
		return strings.TrimSpace(bucket.Name)
	}
	var parts []string
	seen := make(map[string]struct{})
	for id != "" {
		if _, ok := seen[id]; ok {
			break
		}
		seen[id] = struct{}{}
		node, ok := byID[id]
		if !ok {
			name := id
			// Missing ancestor: use the leaf OU name only for the selected bucket itself,
			// not for a parent that is absent from the hierarchy (avoids repeating the leaf name).
			if id == strings.TrimSpace(bucket.ID) {
				if n := strings.TrimSpace(bucket.Name); n != "" {
					name = n
				}
			}
			parts = append(parts, name)
			break
		}
		name := strings.TrimSpace(node.Name)
		if name == "" {
			name = node.ID
		}
		parts = append(parts, name)
		id = strings.TrimSpace(node.ParentID)
	}
	for i, j := 0, len(parts)-1; i < j; i, j = i+1, j-1 {
		parts[i], parts[j] = parts[j], parts[i]
	}
	return strings.Join(parts, " / ")
}
