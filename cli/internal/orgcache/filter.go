// Package orgcache resolves AWS Organizations accounts by tag using the shared finops cache.
package orgcache

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/openshift-online/finops-tools/cli/internal/cache"
	coreaccount "github.com/openshift-online/finops-tools/core/account"
)

const namespace = "org"

var scanOrganizationAccountTags = coreaccount.ScanOrganizationAccountTagsWithProgress

// Options controls organization tag resolution and cache behavior.
type Options struct {
	ConfigPath string
	PayerID    string
	Skip       bool
	Refresh    bool
	TTL        time.Duration
}

// LoadOrganizationAccountTags returns the full organization account+tags scan,
// using the org cache namespace when allowed.
func LoadOrganizationAccountTags(
	ctx context.Context,
	awsCfg aws.Config,
	progress coreaccount.TagFilterProgress,
	opts Options,
) ([]coreaccount.OrganizationAccountTags, error) {
	payerID := strings.TrimSpace(opts.PayerID)
	if payerID == "" {
		return nil, errors.New("payer account ID is required")
	}

	store := cache.New(opts.ConfigPath)
	policy := cache.Policy{
		Skip:    opts.Skip,
		Refresh: opts.Refresh,
		TTL:     opts.TTL,
	}
	hooks := &cache.Hooks[[]coreaccount.OrganizationAccountTags]{
		OnHit: func(entry cache.Entry[[]coreaccount.OrganizationAccountTags]) {
			step(progress, fmt.Sprintf(
				"Using cached organization data (%d accounts, fetched %s)…",
				len(entry.Value),
				entry.FetchedAt.UTC().Format(time.RFC3339),
			))
		},
		OnMiss: func(reason cache.MissReason, err error) {
			switch reason {
			case cache.MissRefresh:
				step(progress, "Refreshing organization cache…")
			case cache.MissExpired:
				step(progress, "Organization cache expired; refreshing…")
			case cache.MissError:
				step(progress, fmt.Sprintf("Organization cache unavailable (%v); fetching live data…", err))
			}
		},
		OnUpdate: func(scan []coreaccount.OrganizationAccountTags) {
			step(progress, fmt.Sprintf("Updated organization cache (%d accounts)", len(scan)))
		},
		OnSaveError: func(err error) {
			step(progress, fmt.Sprintf("Warning: could not update organization cache: %v", err))
		},
	}

	result, err := cache.GetOrFetch(store, namespace, payerID, policy, time.Now().UTC(), hooks, func() ([]coreaccount.OrganizationAccountTags, error) {
		return scanOrganizationAccountTags(ctx, awsCfg, progress)
	}, scanValid)
	if err != nil {
		return nil, err
	}
	return result.Value, nil
}

// FilterOrganizationAccountsByTag resolves accounts by tag, using the org cache namespace when allowed.
func FilterOrganizationAccountsByTag(
	ctx context.Context,
	awsCfg aws.Config,
	tagKey, tagValue string,
	progress coreaccount.TagFilterProgress,
	opts Options,
) ([]coreaccount.OrganizationAccount, error) {
	tagKey = strings.TrimSpace(tagKey)
	if tagKey == "" {
		return nil, fmt.Errorf("tag key is required")
	}

	scan, err := LoadOrganizationAccountTags(ctx, awsCfg, progress, opts)
	if err != nil {
		return nil, err
	}
	return coreaccount.FilterOrganizationAccountsFromScan(scan, tagKey, tagValue, progress), nil
}

func scanValid(scan []coreaccount.OrganizationAccountTags) bool {
	return len(scan) > 0
}

func step(progress coreaccount.TagFilterProgress, message string) {
	if progress == nil {
		return
	}
	progress.Step(message)
}
