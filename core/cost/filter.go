// filter.go removes redundant linked-account targets when their payer is also in the request set.
package cost

import (
	"sort"
	"strings"
)

// FilterOverlappingTargets drops linked accounts whose payer is also requested,
// since linked costs are already included in payer totals.
func FilterOverlappingTargets(targets []AccountTarget) []AccountTarget {
	payers := make(map[string]struct{})
	for _, t := range targets {
		if !t.IsLinked() {
			payers[t.AccountID] = struct{}{}
		}
	}

	out := make([]AccountTarget, 0, len(targets))
	for _, t := range targets {
		if t.IsLinked() {
			if _, ok := payers[t.PayerAccountID]; ok {
				continue
			}
		}
		out = append(out, t)
	}
	return out
}

// GroupByCredentialsAccount groups targets by the account ID whose credentials are used.
func GroupByCredentialsAccount(targets []AccountTarget) map[string][]AccountTarget {
	groups := make(map[string][]AccountTarget)
	for _, t := range targets {
		credID := t.CredentialsAccountID()
		if credID == "" {
			credID = strings.TrimSpace(t.AccountID)
		}
		groups[credID] = append(groups[credID], t)
	}
	return groups
}

// SortedCredentialAccountIDs returns group keys in sorted order for deterministic iteration.
func SortedCredentialAccountIDs(groups map[string][]AccountTarget) []string {
	ids := make([]string, 0, len(groups))
	for id := range groups {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}
