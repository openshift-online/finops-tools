// snapshot_billing.go fetches billed EBS/RDS snapshot storage from Cost Explorer.
package cmd

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/openshift-online/finops-tools/cli/internal/configstore"
	"github.com/openshift-online/finops-tools/core/cost"
	"github.com/openshift-online/finops-tools/core/snapshot"
)

var (
	fetchSnapshotBilledCosts         = fetchSnapshotBilledCostsImpl
	fetchBilledSnapshotCostsForGroup = snapshot.FetchBilledSnapshotCosts
)

func fetchSnapshotBilledCostsImpl(
	ctx context.Context,
	store configstore.File,
	targets []cost.AccountTarget,
	credentialsFile string,
	now time.Time,
) ([]snapshot.AccountBilledSnapshotCosts, error) {
	type group struct {
		accountIDs []string
		seen       map[string]struct{}
	}
	groups := make(map[string]*group)
	groupOrder := make([]string, 0)
	accountOrder := make([]string, 0)
	seenAccount := make(map[string]struct{})

	for _, target := range targets {
		accountID := strings.TrimSpace(target.AccountID)
		if accountID == "" {
			continue
		}
		credID := target.CredentialsAccountID()
		if _, ok := groups[credID]; !ok {
			groups[credID] = &group{seen: make(map[string]struct{})}
			groupOrder = append(groupOrder, credID)
		}
		g := groups[credID]
		if _, ok := g.seen[accountID]; !ok {
			g.seen[accountID] = struct{}{}
			g.accountIDs = append(g.accountIDs, accountID)
		}
		if _, ok := seenAccount[accountID]; !ok {
			seenAccount[accountID] = struct{}{}
			accountOrder = append(accountOrder, accountID)
		}
	}

	byAccount := make(map[string]snapshot.AccountBilledSnapshotCosts, len(accountOrder))
	for _, credID := range groupOrder {
		g := groups[credID]
		awsCfg, err := loadAWSConfigForCredentialsAccount(ctx, store, credID, credentialsFile)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", credID, err)
		}
		billed, err := fetchBilledSnapshotCostsForGroup(
			ctx,
			snapshot.NewCostExplorerClient(awsCfg),
			g.accountIDs,
			now,
		)
		if err != nil {
			return nil, err
		}
		for _, row := range billed {
			byAccount[row.AccountID] = row
		}
	}

	out := make([]snapshot.AccountBilledSnapshotCosts, 0, len(accountOrder))
	for _, accountID := range accountOrder {
		if row, ok := byAccount[accountID]; ok {
			out = append(out, row)
		}
	}
	return out, nil
}
