package awshealth

import (
	"context"
	"errors"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	coreaccount "github.com/openshift-online/finops-tools/core/account"
	"github.com/openshift-online/finops-tools/core/accountreview"
	"github.com/openshift-online/finops-tools/core/parallel"
)

// resolveAccountOwners enriches the filtered findings without assuming member
// roles. Tag lookups are deduplicated by payer/account and bounded to five workers.
func resolveAccountOwners(ctx context.Context, q Query, result *Result) error {
	type accountKey struct{ payerID, accountID string }
	type ownerResult struct{ email, message string }
	configs := make(map[string]aws.Config, len(q.Targets))
	for _, target := range q.Targets {
		if _, exists := configs[target.AccountID]; !exists {
			configs[target.AccountID] = target.AWSConfig
		}
	}
	var keys []accountKey
	indexes := make(map[accountKey]int)
	for _, event := range result.Events {
		for _, account := range event.Accounts {
			key := accountKey{event.PayerAccountID, account.AccountID}
			if _, exists := indexes[key]; !exists {
				indexes[key] = len(keys)
				keys = append(keys, key)
			}
		}
	}
	listTags := q.listTags
	if listTags == nil {
		listTags = coreaccount.ListTags
	}
	owners := make([]ownerResult, len(keys))
	workers := min(parallel.WorkersOrDefault(q.Workers), 5)
	if err := parallel.ForEach(ctx, workers, len(keys), func(ctx context.Context, i int) error {
		key := keys[i]
		tags, err := listTags(ctx, configs[key.payerID], key.accountID)
		if err != nil {
			err = fmt.Errorf("list account tags: %w", err)
		} else {
			owners[i].email, err = accountreview.ResolveOwnerEmail(tags, "")
		}
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return err
		}
		if err != nil {
			owners[i].message = err.Error()
		}
		return ctx.Err()
	}); err != nil {
		return err
	}
	for i := range result.Events {
		event := &result.Events[i]
		for j := range event.Accounts {
			account := &event.Accounts[j]
			owner := owners[indexes[accountKey{event.PayerAccountID, account.AccountID}]]
			account.OwnerEmail, account.OwnerError = owner.email, owner.message
			if owner.message != "" {
				result.Warnings = append(result.Warnings, Warning{
					PayerAccountID: event.PayerAccountID, EventARN: event.ARN,
					AccountID: account.AccountID, Operation: "account_owner", Message: owner.message,
				})
			}
		}
	}
	return nil
}
