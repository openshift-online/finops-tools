package extendedsupport

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/health"
	healthtypes "github.com/aws/aws-sdk-go-v2/service/health/types"
	"github.com/openshift-online/finops-tools/core/apilog"
)

// HealthAPI is the AWS Health version catalog API used for lifecycle discovery.
type HealthAPI interface {
	DescribeServiceLifecycle(context.Context, *health.DescribeServiceLifecycleInput, ...func(*health.Options)) (*health.DescribeServiceLifecycleOutput, error)
}

type healthVersionKey struct {
	service         Service
	engine, version string
}

// HealthLifecycleCatalog caches one complete, immutable catalog for a command run.
// It is shared across accounts, regions, and the skipped-account retry. Failed
// requests are cached by credential source so member accounts do not each repeat
// a denied payer request. The zero value is usable; do not copy after first use.
type HealthLifecycleCatalog struct {
	mu        sync.Mutex
	entries   map[healthVersionKey][]healthtypes.LifecycleEvent
	pending   chan struct{}
	failures  map[string]error
	err       error
	newClient func(aws.Config) HealthAPI
}

func NewHealthLifecycleCatalog() *HealthLifecycleCatalog { return &HealthLifecycleCatalog{} }

func (c *HealthLifecycleCatalog) load(ctx context.Context, cfg aws.Config, source string) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		c.mu.Lock()
		if c.entries != nil {
			c.mu.Unlock()
			return nil
		}
		if pending := c.pending; pending != nil {
			c.mu.Unlock()
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-pending:
				continue
			}
		}
		if err := c.failures[source]; err != nil {
			c.mu.Unlock()
			return err
		}
		c.pending = make(chan struct{})
		c.mu.Unlock()
		regional := cfg.Copy()
		// Health's active commercial endpoint is in us-east-1; event regions in
		// the returned catalog determine applicability to resource scan regions.
		regional.Region = "us-east-1"
		var client HealthAPI
		if c.newClient != nil {
			client = c.newClient(regional)
		} else {
			client = apilog.WrapHealth(health.NewFromConfig(regional))
		}
		entries, err := loadHealthLifecycles(ctx, client)
		c.mu.Lock()
		if err == nil {
			c.entries, c.err = entries, nil
		} else if ctx.Err() == nil && !errors.Is(err, context.Canceled) {
			if c.failures == nil {
				c.failures = make(map[string]error)
			}
			c.failures[source] = err
			c.err = fmt.Errorf("credential source %s: %w", source, err)
		}
		close(c.pending)
		c.pending = nil
		c.mu.Unlock()
		return err
	}
}

func loadHealthLifecycles(ctx context.Context, client HealthAPI) (map[healthVersionKey][]healthtypes.LifecycleEvent, error) {
	entries := make(map[healthVersionKey][]healthtypes.LifecycleEvent)
	seen := make(map[string]bool)
	var token *string
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		// Read the complete service-wide catalog rather than guessing the service
		// filter identifiers. Only ElastiCache and OpenSearch entries are retained.
		out, err := client.DescribeServiceLifecycle(ctx, &health.DescribeServiceLifecycleInput{MaxResults: aws.Int32(20), NextToken: token})
		if err != nil {
			if strings.TrimSpace(err.Error()) == "" {
				return nil, fmt.Errorf("Health.DescribeServiceLifecycle returned no error message (%T): %w", err, err)
			}
			return nil, fmt.Errorf("Health.DescribeServiceLifecycle: %w", err)
		}
		if out == nil {
			return nil, fmt.Errorf("Health.DescribeServiceLifecycle returned no response")
		}
		for _, entry := range out.ServiceLifecycles {
			key, ok := healthEntryKey(entry)
			if !ok {
				continue
			}
			if _, exists := entries[key]; !exists {
				entries[key] = []healthtypes.LifecycleEvent{}
			}
			entries[key] = append(entries[key], entry.LifecycleEvents...)
		}
		if aws.ToString(out.NextToken) == "" {
			return entries, nil
		}
		if seen[aws.ToString(out.NextToken)] {
			return nil, fmt.Errorf("Health.DescribeServiceLifecycle returned a repeated pagination token")
		}
		seen[aws.ToString(out.NextToken)] = true
		token = out.NextToken
	}
}

func healthEntryKey(entry healthtypes.ServiceLifecycle) (healthVersionKey, bool) {
	var service Service
	switch normalizeLifecycleName(aws.ToString(entry.Service)) {
	case "ELASTICACHE":
		service = ServiceElastiCache
	case "OPENSEARCH", "OPENSEARCH_SERVICE", "ES":
		service = ServiceOpenSearch
	default:
		return healthVersionKey{}, false
	}
	raw := strings.ToLower(strings.TrimSpace(aws.ToString(entry.Version)))
	engine, version := "", raw
	for _, separator := range []string{"/", "_", ":"} {
		if before, after, ok := strings.Cut(raw, separator); ok {
			engine, version = before, after
			break
		}
	}
	if engine == "" {
		title := strings.ToLower(aws.ToString(entry.Title))
		for _, candidate := range []string{"elasticsearch", "opensearch", "redis", "valkey", "memcached"} {
			if strings.Contains(title, candidate) {
				engine = candidate
				break
			}
		}
	}
	if engine == "redis-oss" || engine == "redisoss" {
		engine = "redis"
	}
	if service == ServiceElastiCache && engine != "redis" && engine != "valkey" && engine != "memcached" {
		return healthVersionKey{}, false
	}
	if service == ServiceOpenSearch && engine != "opensearch" && engine != "elasticsearch" {
		return healthVersionKey{}, false
	}
	version = strings.TrimSpace(version)
	if version == "" {
		return healthVersionKey{}, false
	}
	return healthVersionKey{service: service, engine: engine, version: version}, true
}

func (c *HealthLifecycleCatalog) preload(ctx context.Context, q Query) error {
	seen := make(map[string]bool)
	for _, target := range q.Targets {
		c.mu.Lock()
		ready := c.entries != nil
		c.mu.Unlock()
		if ready {
			return nil
		}
		source := strings.TrimSpace(target.CatalogAccountID)
		if source == "" {
			source = strings.TrimSpace(target.AccountID)
		}
		if source == "" || seen[source] {
			continue
		}
		seen[source] = true
		cfg := target.AWSConfig
		loader := target.CatalogConfigLoader
		if loader == nil {
			loader = target.ConfigLoader
		}
		var err error
		if loader != nil {
			cfg, err = loader(ctx)
		}
		if err == nil {
			err = c.load(ctx, cfg, source)
		}
		if errors.Is(err, context.Canceled) || ctx.Err() != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return err
		}
		if err != nil {
			c.mu.Lock()
			c.err = fmt.Errorf("credential source %s: %w", source, err)
			c.mu.Unlock()
			apilog.Log(ctx, fmt.Sprintf("AWS Health lifecycle catalog source=%s: %s", source, warningMessage(err)))
		}
	}
	return nil
}

func (c *HealthLifecycleCatalog) warnings() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.entries != nil {
		return nil
	}
	reason := "no usable catalog credential source"
	if c.err != nil {
		reason = warningMessage(c.err)
	}
	return []string{"AWS Health lifecycle catalog unavailable: " + reason + "; ElastiCache/OpenSearch support status cannot be confirmed. Check Health API permissions and AWS Support plan access."}
}

func (c *HealthLifecycleCatalog) retryFailures() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.failures = nil
	c.err = nil
}
