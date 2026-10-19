package extendedsupport

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/eks"
	ekstypes "github.com/aws/aws-sdk-go-v2/service/eks/types"
)

// EKSAPI is the subset of EKS used for cluster and support-version discovery.
type EKSAPI interface {
	ListClusters(context.Context, *eks.ListClustersInput, ...func(*eks.Options)) (*eks.ListClustersOutput, error)
	DescribeCluster(context.Context, *eks.DescribeClusterInput, ...func(*eks.Options)) (*eks.DescribeClusterOutput, error)
	DescribeClusterVersions(context.Context, *eks.DescribeClusterVersionsInput, ...func(*eks.Options)) (*eks.DescribeClusterVersionsOutput, error)
}

type eksLister interface {
	ListEKSExtendedSupport(context.Context, aws.Config, string, string, time.Time) (regionScanResult, error)
}

type awsEKSLister struct{ catalog *EKSVersionCatalog }

func (l awsEKSLister) ListEKSExtendedSupport(ctx context.Context, cfg aws.Config, region, accountID string, now time.Time) (regionScanResult, error) {
	regionalCfg := cfg.Copy()
	regionalCfg.Region = region
	client := l.catalog.client(regionalCfg)
	var result regionScanResult
	names, err := listEKSClusters(ctx, client)
	if errors.Is(err, context.Canceled) {
		return result, err
	}
	if ctx.Err() != nil {
		return result, ctx.Err()
	}
	if err != nil {
		result.Warnings = append(result.Warnings, RegionWarning{
			AccountID: accountID, Region: region, Service: ServiceEKS,
			Message: fmt.Sprintf("EKS cluster discovery incomplete: %v", err),
		})
	}
	if len(names) == 0 {
		return result, nil
	}
	versions := l.catalog.load(ctx, cfg, region, accountID)
	if errors.Is(versions.err, context.Canceled) {
		return result, versions.err
	}
	if ctx.Err() != nil {
		return result, ctx.Err()
	}
	for _, name := range names {
		warning := ResourceWarning{
			AccountID: accountID, Region: region, Service: ServiceEKS,
			ResourceType: ResourceTypeEKSCluster, ResourceID: name, Engine: "kubernetes",
		}
		out, err := client.DescribeCluster(ctx, &eks.DescribeClusterInput{Name: aws.String(name)})
		if errors.Is(err, context.Canceled) {
			return result, err
		}
		if ctx.Err() != nil {
			return result, ctx.Err()
		}
		if err != nil {
			warning.Message = fmt.Sprintf("EKS.DescribeCluster failed for %s: %v", name, err)
		} else if out == nil || out.Cluster == nil {
			warning.Message = "EKS.DescribeCluster returned no cluster information"
		} else {
			cluster := out.Cluster
			warning.EngineVersion = strings.TrimSpace(aws.ToString(cluster.Version))
			version, ok := versions.versions[warning.EngineVersion]
			switch {
			case warning.EngineVersion == "":
				warning.Message = "cluster Kubernetes version is missing"
			case !ok && versions.err != nil:
				warning.Message = fmt.Sprintf("EKS version catalog lookup failed: %v", versions.err)
			case !ok:
				warning.Message = fmt.Sprintf("EKS lifecycle information for Kubernetes version %s is missing", warning.EngineVersion)
			case version.EndOfStandardSupportDate == nil || version.EndOfStandardSupportDate.IsZero():
				warning.Message = "standard support end date is missing"
			case version.EndOfExtendedSupportDate != nil && version.EndOfExtendedSupportDate.IsZero():
				warning.Message = "extended support end date is missing"
			case version.EndOfExtendedSupportDate != nil && !version.EndOfExtendedSupportDate.After(*version.EndOfStandardSupportDate):
				warning.Message = "EKS support dates are inconsistent"
			case version.VersionStatus == ekstypes.VersionStatusUnsupported || (version.EndOfExtendedSupportDate != nil && !now.Before(*version.EndOfExtendedSupportDate)):
				warning.Message = "Kubernetes version is past extended support"
			case now.Before(*version.EndOfStandardSupportDate):
				continue
			case version.EndOfExtendedSupportDate == nil || version.EndOfExtendedSupportDate.IsZero():
				warning.Message = "extended support end date is missing"
			default:
				support := ""
				if cluster.UpgradePolicy != nil {
					support = string(cluster.UpgradePolicy.SupportType)
				}
				result.Records = append(result.Records, Record{
					AccountID: accountID, Region: region, Service: ServiceEKS,
					ResourceType: ResourceTypeEKSCluster, ResourceID: name, Engine: "kubernetes",
					EngineVersion: warning.EngineVersion, MajorEngineVersion: warning.EngineVersion,
					StandardSupportEndDate: version.EndOfStandardSupportDate.UTC(),
					ExtendedSupportEndDate: version.EndOfExtendedSupportDate,
					EngineLifecycleSupport: support, Tags: maps.Clone(cluster.Tags),
				})
				continue
			}
		}
		result.UnclassifiedResources = append(result.UnclassifiedResources, warning)
	}
	return result, nil
}

func listEKSClusters(ctx context.Context, client EKSAPI) ([]string, error) {
	var names []string
	seen := make(map[string]bool)
	seenTokens := make(map[string]bool)
	var token *string
	for {
		// Leaving Include unset excludes EKS Connector external clusters.
		out, err := client.ListClusters(ctx, &eks.ListClustersInput{NextToken: token})
		if err != nil {
			return names, fmt.Errorf("EKS.ListClusters: %w", err)
		}
		if out == nil {
			return names, fmt.Errorf("EKS.ListClusters returned no response")
		}
		for _, name := range out.Clusters {
			name = strings.TrimSpace(name)
			if name != "" && !seen[name] {
				names = append(names, name)
				seen[name] = true
			}
		}
		if aws.ToString(out.NextToken) == "" {
			return names, nil
		}
		if seenTokens[aws.ToString(out.NextToken)] {
			return names, fmt.Errorf("EKS.ListClusters returned a repeated pagination token")
		}
		seenTokens[aws.ToString(out.NextToken)] = true
		token = out.NextToken
	}
}
