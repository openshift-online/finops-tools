package inventory

import (
	"context"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
)

type selectedInventoryRegions struct{}

func (selectedInventoryRegions) ListEnabledRegions(_ context.Context, cfg aws.Config, requested []string) ([]string, error) {
	if len(requested) > 0 {
		return requested, nil
	}
	return []string{cfg.Region, "us-west-2"}, nil
}

func TestScanExcludesRegionalCallsAndKeepsGlobalInventory(t *testing.T) {
	for _, tc := range []struct {
		name    string
		regions []string
		want    int
	}{
		{name: "discovered per account", want: 8},
		{name: "explicit", regions: []string{"us-east-1", "us-west-2"}, want: 8},
		{name: "all excluded", regions: []string{"us-west-2"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			installEmptyInventoryListers(t)
			var mu sync.Mutex
			var calls []string
			record := func(service, region string) {
				mu.Lock()
				defer mu.Unlock()
				calls = append(calls, service+":"+region)
			}
			listRegionalEC2 = func(_ context.Context, _ EC2API, region string) ([]EC2Instance, []EBSVolume, []ElasticIP, []NATGateway, []VPC, error) {
				record("ec2", region)
				return nil, nil, nil, nil, nil, nil
			}
			listRegionalRDS = func(_ context.Context, _ RDSAPI, region string) ([]RDSInstance, []RDSCluster, error) {
				record("rds", region)
				return nil, nil, nil
			}
			listRegionalLBs = func(_ context.Context, _ ELBV2API, _ ELBAPI, region string) ([]LoadBalancer, error) {
				record("elb", region)
				return nil, nil
			}
			listRegionalLambda = func(_ context.Context, _ LambdaAPI, region string) ([]LambdaFunction, error) {
				record("lambda", region)
				return nil, nil
			}
			var globalCalls atomic.Int32
			listGlobalRoute53 = func(context.Context, Route53API) ([]HostedZone, error) {
				globalCalls.Add(1)
				return nil, nil
			}
			listGlobalS3 = func(context.Context, S3API) ([]S3Bucket, error) {
				globalCalls.Add(1)
				return []S3Bucket{{Name: "global-bucket", Region: "us-west-2"}}, nil
			}
			result, err := Scan(context.Background(), Query{
				Targets: []AccountTarget{
					{AccountID: "111111111111", AWSConfig: aws.Config{Region: "us-east-1"}},
					{AccountID: "222222222222", AWSConfig: aws.Config{Region: "eu-west-1"}},
				},
				Regions: tc.regions, SkipRegions: []string{" us-west-2 ", "us-west-2"},
				regionLister: selectedInventoryRegions{},
			})
			if err != nil {
				t.Fatal(err)
			}
			if len(calls) != tc.want || globalCalls.Load() != 4 {
				t.Fatalf("regional=%v global=%d", calls, globalCalls.Load())
			}
			for _, call := range calls {
				if strings.HasSuffix(call, ":us-west-2") {
					t.Fatalf("called excluded region: %s", call)
				}
			}
			if tc.name == "discovered per account" {
				found := false
				for _, call := range calls {
					found = found || call == "ec2:eu-west-1"
				}
				if !found {
					t.Fatalf("member-specific region lost: %v", calls)
				}
			}
			for _, inv := range result.Accounts {
				if !reflect.DeepEqual(inv.ExcludedRegions, []string{"us-west-2"}) || len(inv.SkippedRegions) != 0 || len(inv.Warnings) != 0 || len(inv.S3Buckets) != 1 {
					t.Fatalf("inventory = %+v", inv)
				}
			}
		})
	}
}
