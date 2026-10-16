package extendedsupport

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
)

type regionLister interface {
	ListEnabledRegions(ctx context.Context, cfg aws.Config, requested []string) ([]string, error)
}

type ec2RegionLister struct{}

func (ec2RegionLister) ListEnabledRegions(ctx context.Context, cfg aws.Config, requested []string) ([]string, error) {
	return listEnabledRegionsWithClient(ctx, newEC2Client(awsConfigWithDefaultRegion(cfg)), requested)
}

// EC2API is the subset of EC2 used for region discovery.
type EC2API interface {
	DescribeRegions(
		ctx context.Context,
		params *ec2.DescribeRegionsInput,
		optFns ...func(*ec2.Options),
	) (*ec2.DescribeRegionsOutput, error)
}

func newEC2Client(cfg aws.Config) EC2API {
	return ec2.NewFromConfig(cfg)
}

func listEnabledRegionsWithClient(ctx context.Context, client EC2API, requested []string) ([]string, error) {
	if len(requested) > 0 {
		out := make([]string, 0, len(requested))
		seen := make(map[string]struct{})
		for _, region := range requested {
			region = strings.TrimSpace(region)
			if region == "" {
				continue
			}
			if _, ok := seen[region]; ok {
				continue
			}
			seen[region] = struct{}{}
			out = append(out, region)
		}
		if len(out) == 0 {
			return nil, fmt.Errorf("at least one region is required")
		}
		sort.Strings(out)
		return out, nil
	}

	out, err := client.DescribeRegions(ctx, &ec2.DescribeRegionsInput{
		AllRegions: aws.Bool(false),
	})
	if err != nil {
		return nil, err
	}
	regions := make([]string, 0, len(out.Regions))
	for _, region := range out.Regions {
		if name := strings.TrimSpace(aws.ToString(region.RegionName)); name != "" {
			regions = append(regions, name)
		}
	}
	sort.Strings(regions)
	return regions, nil
}

func awsConfigWithDefaultRegion(cfg aws.Config) aws.Config {
	if cfg.Region != "" {
		return cfg
	}
	out := cfg.Copy()
	out.Region = "us-east-1"
	return out
}
