package extendedsupport

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/eks"
	ekstypes "github.com/aws/aws-sdk-go-v2/service/eks/types"
	"github.com/openshift-online/finops-tools/core/apilog"
	"github.com/openshift-online/finops-tools/core/parallel"
)

type eksCatalogResult struct {
	versions map[string]ekstypes.ClusterVersionInformation
	err      error
}

type eksCatalogRegion struct {
	versions map[string]ekstypes.ClusterVersionInformation
	pending  chan struct{}
	// Failed credentials must not prevent another account from loading the catalog.
	failures map[string]eksCatalogResult
}

// EKSVersionCatalog holds immutable regional version catalogs for one run.
// Concurrent callers share successful loads; failed loads are retained only for
// their credential source, preventing a failed API call for every cluster.
// Its zero value is ready for use. Do not copy a catalog after first use.
type EKSVersionCatalog struct {
	mu        sync.Mutex
	regions   map[string]*eksCatalogRegion
	newClient func(aws.Config) EKSAPI
}

// NewEKSVersionCatalog creates a catalog that can be shared with the retry pass.
func NewEKSVersionCatalog() *EKSVersionCatalog {
	return &EKSVersionCatalog{}
}

func (c *EKSVersionCatalog) client(cfg aws.Config) EKSAPI {
	if c.newClient != nil {
		return c.newClient(cfg)
	}
	return apilog.WrapEKS(eks.NewFromConfig(cfg))
}

func (c *EKSVersionCatalog) load(ctx context.Context, cfg aws.Config, region, sourceAccountID string) eksCatalogResult {
	for {
		if err := ctx.Err(); err != nil {
			return eksCatalogResult{err: err}
		}
		c.mu.Lock()
		if c.regions == nil {
			c.regions = make(map[string]*eksCatalogRegion)
		}
		entry := c.regions[region]
		if entry == nil {
			entry = &eksCatalogRegion{failures: make(map[string]eksCatalogResult)}
			c.regions[region] = entry
		}
		if entry.versions != nil {
			result := eksCatalogResult{versions: entry.versions}
			c.mu.Unlock()
			return result
		}
		if pending := entry.pending; pending != nil {
			c.mu.Unlock()
			select {
			case <-ctx.Done():
				return eksCatalogResult{err: ctx.Err()}
			case <-pending:
				continue
			}
		}
		if failed, ok := entry.failures[sourceAccountID]; ok {
			c.mu.Unlock()
			return failed
		}
		entry.pending = make(chan struct{})
		c.mu.Unlock()

		regionalCfg := cfg.Copy()
		regionalCfg.Region = region
		result := loadEKSVersions(ctx, c.client(regionalCfg))
		c.mu.Lock()
		if result.err == nil {
			entry.versions = result.versions
		} else if ctx.Err() == nil && !errors.Is(result.err, context.Canceled) {
			entry.failures[sourceAccountID] = result
		}
		close(entry.pending)
		entry.pending = nil
		c.mu.Unlock()
		return result
	}
}

func loadEKSVersions(ctx context.Context, client EKSAPI) eksCatalogResult {
	result := eksCatalogResult{versions: make(map[string]ekstypes.ClusterVersionInformation)}
	seenTokens := make(map[string]bool)
	var token *string
	for {
		out, err := client.DescribeClusterVersions(ctx, &eks.DescribeClusterVersionsInput{
			IncludeAll: aws.Bool(true), NextToken: token,
		})
		if err != nil {
			result.err = fmt.Errorf("EKS.DescribeClusterVersions: %w", err)
			return result
		}
		if out == nil {
			result.err = fmt.Errorf("EKS.DescribeClusterVersions returned no response")
			return result
		}
		for _, version := range out.ClusterVersions {
			id := strings.TrimSpace(aws.ToString(version.ClusterVersion))
			if id != "" {
				result.versions[id] = version
			}
		}
		if aws.ToString(out.NextToken) == "" {
			return result
		}
		if seenTokens[aws.ToString(out.NextToken)] {
			result.err = fmt.Errorf("EKS.DescribeClusterVersions returned a repeated pagination token")
			return result
		}
		seenTokens[aws.ToString(out.NextToken)] = true
		token = out.NextToken
	}
}

// preload authenticates only catalog sources (normally payers), not every member.
// Failures remain recoverable through member credentials when scanning starts.
func (c *EKSVersionCatalog) preload(ctx context.Context, q Query) error {
	seen := make(map[string]bool)
	var sources []AccountTarget
	for _, target := range q.Targets {
		target.CatalogAccountID = strings.TrimSpace(target.CatalogAccountID)
		if target.CatalogAccountID == "" {
			target.CatalogAccountID = strings.TrimSpace(target.AccountID)
		}
		if target.CatalogConfigLoader == nil || seen[target.CatalogAccountID] {
			continue
		}
		seen[target.CatalogAccountID] = true
		sources = append(sources, target)
	}
	return parallel.ForEach(ctx, q.Workers, len(sources), func(ctx context.Context, i int) error {
		source := sources[i]
		cfg, err := source.CatalogConfigLoader(ctx)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return err
			}
			if ctx.Err() != nil {
				return ctx.Err()
			}
			apilog.Log(ctx, fmt.Sprintf("EKS catalog credentials account=%s: %v", source.CatalogAccountID, err))
			return nil
		}
		regions, err := q.regionLister.ListEnabledRegions(ctx, cfg, q.Regions)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return err
			}
			if ctx.Err() != nil {
				return ctx.Err()
			}
			apilog.Log(ctx, fmt.Sprintf("EKS catalog region discovery account=%s: %v", source.CatalogAccountID, err))
			return nil
		}
		return parallel.ForEach(ctx, defaultRegionConcurrency, len(regions), func(ctx context.Context, i int) error {
			region := regions[i]
			apilog.Log(ctx, fmt.Sprintf("Preloading EKS versions account=%s region=%s", source.CatalogAccountID, region))
			result := c.load(ctx, cfg, region, source.CatalogAccountID)
			if errors.Is(result.err, context.Canceled) {
				return result.err
			}
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if result.err != nil {
				apilog.Log(ctx, fmt.Sprintf("EKS catalog preload account=%s region=%s: %v", source.CatalogAccountID, region, result.err))
			}
			return nil
		})
	})
}
