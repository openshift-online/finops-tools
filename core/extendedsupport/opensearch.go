package extendedsupport

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/opensearch"
	"github.com/openshift-online/finops-tools/core/apilog"
)

// OpenSearchAPI is the subset used to discover managed domains and their tags.
type OpenSearchAPI interface {
	ListDomainNames(context.Context, *opensearch.ListDomainNamesInput, ...func(*opensearch.Options)) (*opensearch.ListDomainNamesOutput, error)
	DescribeDomain(context.Context, *opensearch.DescribeDomainInput, ...func(*opensearch.Options)) (*opensearch.DescribeDomainOutput, error)
	ListTags(context.Context, *opensearch.ListTagsInput, ...func(*opensearch.Options)) (*opensearch.ListTagsOutput, error)
}

type openSearchLister interface {
	ListOpenSearchExtendedSupport(context.Context, aws.Config, string, string, time.Time) (regionScanResult, error)
}

type awsOpenSearchLister struct {
	newClient func(aws.Config) OpenSearchAPI
	catalog   *HealthLifecycleCatalog
}

func (l awsOpenSearchLister) ListOpenSearchExtendedSupport(ctx context.Context, cfg aws.Config, region, accountID string, now time.Time) (regionScanResult, error) {
	regionalCfg := cfg.Copy()
	regionalCfg.Region = region
	var client OpenSearchAPI
	if l.newClient != nil {
		client = l.newClient(regionalCfg)
	} else {
		client = apilog.WrapOpenSearch(opensearch.NewFromConfig(regionalCfg))
	}
	var result regionScanResult
	if err := ctx.Err(); err != nil {
		return result, err
	}
	// ListDomainNames is not paginated. Leave EngineType unset to include both
	// OpenSearch and legacy Elasticsearch domains. Serverless uses a separate API
	// and has no customer-selected engine versions to classify.
	out, err := client.ListDomainNames(ctx, &opensearch.ListDomainNamesInput{})
	if ctx.Err() != nil {
		return result, ctx.Err()
	}
	if err != nil {
		return result, fmt.Errorf("OpenSearch.ListDomainNames: %w", err)
	}
	if out == nil {
		return result, fmt.Errorf("OpenSearch.ListDomainNames returned no response")
	}
	seen := make(map[string]bool)
	for _, domain := range out.DomainNames {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		name := strings.TrimSpace(aws.ToString(domain.DomainName))
		warning := ResourceWarning{AccountID: accountID, Region: region, Service: ServiceOpenSearch, ResourceType: ResourceTypeOpenSearchDomain, ResourceID: name}
		if name == "" {
			warning.Message = "OpenSearch.ListDomainNames returned a domain with no name"
			result.UnclassifiedResources = append(result.UnclassifiedResources, warning)
			continue
		}
		if seen[name] {
			continue
		}
		seen[name] = true
		described, err := client.DescribeDomain(ctx, &opensearch.DescribeDomainInput{DomainName: aws.String(name)})
		if errors.Is(err, context.Canceled) || ctx.Err() != nil {
			if ctx.Err() != nil {
				return result, ctx.Err()
			}
			return result, err
		}
		if err != nil {
			warning.Message = fmt.Sprintf("OpenSearch.DescribeDomain for %s: %s", name, warningMessage(err))
		} else if described == nil || described.DomainStatus == nil {
			warning.Message = "OpenSearch.DescribeDomain returned no domain information"
		} else {
			status := described.DomainStatus
			if aws.ToBool(status.Deleted) {
				continue
			}
			if returned := strings.TrimSpace(aws.ToString(status.DomainName)); returned != "" && returned != name {
				warning.Message = fmt.Sprintf("OpenSearch.DescribeDomain for %s returned information for %s", name, returned)
			} else {
				record, unknown := classifyOpenSearch(accountID, region, name, aws.ToString(status.EngineVersion), now, l.catalog)
				if unknown != nil {
					result.UnclassifiedResources = append(result.UnclassifiedResources, *unknown)
				}
				if record == nil {
					continue
				}
				tags, tagErr := loadOpenSearchTags(ctx, client, aws.ToString(status.ARN))
				if errors.Is(tagErr, context.Canceled) || ctx.Err() != nil {
					if ctx.Err() != nil {
						return result, ctx.Err()
					}
					return result, tagErr
				}
				record.Tags = tags
				if tagErr != nil {
					result.Warnings = append(result.Warnings, RegionWarning{
						AccountID: accountID, Region: region, Service: ServiceOpenSearch,
						Message: fmt.Sprintf("OpenSearch.ListTags for %s: %s (resource retained without tags)", name, warningMessage(tagErr)),
					})
				}
				result.Records = append(result.Records, *record)
				continue
			}
		}
		result.UnclassifiedResources = append(result.UnclassifiedResources, warning)
	}
	return result, nil
}

func loadOpenSearchTags(ctx context.Context, client OpenSearchAPI, arn string) (map[string]string, error) {
	if strings.TrimSpace(arn) == "" {
		return nil, fmt.Errorf("domain ARN is missing")
	}
	out, err := client.ListTags(ctx, &opensearch.ListTagsInput{ARN: aws.String(arn)})
	if err != nil {
		return nil, err
	}
	if out == nil {
		return nil, fmt.Errorf("returned no response")
	}
	tags := make(map[string]string, len(out.TagList))
	for _, tag := range out.TagList {
		if key := aws.ToString(tag.Key); key != "" {
			tags[key] = aws.ToString(tag.Value)
		}
	}
	return tags, nil
}
