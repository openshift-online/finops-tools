package inventory

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awshttp "github.com/aws/aws-sdk-go-v2/aws/transport/http"
	"github.com/aws/aws-sdk-go-v2/service/lambda"
	lambdatypes "github.com/aws/aws-sdk-go-v2/service/lambda/types"
)

func TestScanRequiresTarget(t *testing.T) {
	t.Parallel()
	_, err := Scan(context.Background(), Query{})
	if err == nil {
		t.Fatal("expected error")
	}
}

type stubAccountScanner struct {
	errByAccount map[string]error
}

func (s stubAccountScanner) scanAccount(_ context.Context, _ Query, target AccountTarget) (AccountInventory, error) {
	if err := s.errByAccount[target.AccountID]; err != nil {
		return AccountInventory{}, err
	}
	return AccountInventory{
		AccountID: target.AccountID,
		S3Buckets: []S3Bucket{{Name: "ok", Region: "us-east-1"}},
	}, nil
}

func TestScanContinuesWhenOneAccountFails(t *testing.T) {
	t.Parallel()
	result, err := Scan(context.Background(), Query{
		Targets: []AccountTarget{
			{AccountID: "111111111111"},
			{AccountID: "222222222222"},
		},
		Workers: 1,
		scanner: stubAccountScanner{errByAccount: map[string]error{
			"111111111111": fmt.Errorf("list regions: access denied"),
		}},
	})
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	if len(result.Accounts) != 2 {
		t.Fatalf("accounts = %d, want 2", len(result.Accounts))
	}
	if result.Accounts[0].AccountID != "111111111111" {
		t.Fatalf("first account = %q", result.Accounts[0].AccountID)
	}
	if len(result.Accounts[0].Warnings) != 1 || !strings.Contains(result.Accounts[0].Warnings[0], "list regions") {
		t.Fatalf("first warnings = %+v", result.Accounts[0].Warnings)
	}
	if result.Accounts[1].AccountID != "222222222222" || len(result.Accounts[1].S3Buckets) != 1 {
		t.Fatalf("second account = %+v", result.Accounts[1])
	}
}

func TestScanRecordsConfigLoaderErrorWithoutAbortingBatch(t *testing.T) {
	installEmptyInventoryListers(t)

	assumeErr := fmt.Errorf("111111111111: assume-role denied")
	result, err := Scan(context.Background(), Query{
		Targets: []AccountTarget{
			{
				AccountID: "111111111111",
				ConfigLoader: func(context.Context) (aws.Config, error) {
					return aws.Config{}, assumeErr
				},
			},
			{
				AccountID: "222222222222",
				ConfigLoader: func(context.Context) (aws.Config, error) {
					return aws.Config{Region: "us-east-1"}, nil
				},
			},
		},
		Workers:      1,
		regionLister: stubRegionLister{regions: []string{"us-east-1"}},
	})
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	if len(result.Accounts) != 2 {
		t.Fatalf("accounts = %d, want 2", len(result.Accounts))
	}
	failed := result.Accounts[0]
	if failed.AccountID != "111111111111" {
		t.Fatalf("first account = %q", failed.AccountID)
	}
	if len(failed.Warnings) != 1 || !strings.Contains(failed.Warnings[0], "assume-role denied") {
		t.Fatalf("first warnings = %+v", failed.Warnings)
	}
	if len(failed.S3Buckets) != 0 || len(failed.EC2Instances) != 0 {
		t.Fatalf("failed account should not have inventory: %+v", failed)
	}
	ok := result.Accounts[1]
	if ok.AccountID != "222222222222" {
		t.Fatalf("second account = %q", ok.AccountID)
	}
	if len(ok.Warnings) != 0 {
		t.Fatalf("second warnings = %+v, want none", ok.Warnings)
	}
}

func TestScanReportsProgressOnCompletion(t *testing.T) {
	t.Parallel()
	var mu sync.Mutex
	var messages []string
	_, err := Scan(context.Background(), Query{
		Targets: []AccountTarget{
			{AccountID: "111111111111", DisplayName: "a"},
			{AccountID: "222222222222", DisplayName: "b"},
		},
		Workers: 1,
		scanner: stubAccountScanner{},
		OnProgress: func(msg string) {
			mu.Lock()
			messages = append(messages, msg)
			mu.Unlock()
		},
	})
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	if len(messages) != 2 {
		t.Fatalf("progress = %#v", messages)
	}
	if !strings.Contains(messages[0], "Scanned inventory for a (111111111111) [1/2]") {
		t.Fatalf("first = %q", messages[0])
	}
	if !strings.Contains(messages[1], "Scanned inventory for b (222222222222) [2/2]") {
		t.Fatalf("second = %q", messages[1])
	}
}

type stubRegionLister struct{ regions []string }

func (s stubRegionLister) ListEnabledRegions(context.Context, aws.Config, []string) ([]string, error) {
	return s.regions, nil
}

func installEmptyInventoryListers(t *testing.T) {
	t.Helper()
	origEC2 := listRegionalEC2
	origRDS := listRegionalRDS
	origLBs := listRegionalLBs
	origLambda := listRegionalLambda
	origR53 := listGlobalRoute53
	origS3 := listGlobalS3
	t.Cleanup(func() {
		listRegionalEC2 = origEC2
		listRegionalRDS = origRDS
		listRegionalLBs = origLBs
		listRegionalLambda = origLambda
		listGlobalRoute53 = origR53
		listGlobalS3 = origS3
	})
	listRegionalEC2 = func(context.Context, EC2API, string) ([]EC2Instance, []EBSVolume, []ElasticIP, []NATGateway, []VPC, error) {
		return nil, nil, nil, nil, nil, nil
	}
	listRegionalRDS = func(context.Context, RDSAPI, string) ([]RDSInstance, []RDSCluster, error) {
		return nil, nil, nil
	}
	listRegionalLBs = func(context.Context, ELBV2API, ELBAPI, string) ([]LoadBalancer, error) {
		return nil, nil
	}
	listRegionalLambda = func(context.Context, LambdaAPI, string) ([]LambdaFunction, error) {
		return nil, nil
	}
	listGlobalRoute53 = func(context.Context, Route53API) ([]HostedZone, error) {
		return nil, nil
	}
	listGlobalS3 = func(context.Context, S3API) ([]S3Bucket, error) {
		return nil, nil
	}
}

func TestScanReportsRegionProgress(t *testing.T) {
	origEC2 := listRegionalEC2
	origRDS := listRegionalRDS
	origLBs := listRegionalLBs
	origLambda := listRegionalLambda
	origR53 := listGlobalRoute53
	origS3 := listGlobalS3
	t.Cleanup(func() {
		listRegionalEC2 = origEC2
		listRegionalRDS = origRDS
		listRegionalLBs = origLBs
		listRegionalLambda = origLambda
		listGlobalRoute53 = origR53
		listGlobalS3 = origS3
	})
	listRegionalEC2 = func(context.Context, EC2API, string) ([]EC2Instance, []EBSVolume, []ElasticIP, []NATGateway, []VPC, error) {
		return nil, nil, nil, nil, nil, nil
	}
	listRegionalRDS = func(context.Context, RDSAPI, string) ([]RDSInstance, []RDSCluster, error) {
		return nil, nil, nil
	}
	listRegionalLBs = func(context.Context, ELBV2API, ELBAPI, string) ([]LoadBalancer, error) {
		return nil, nil
	}
	listRegionalLambda = func(context.Context, LambdaAPI, string) ([]LambdaFunction, error) {
		return nil, nil
	}
	listGlobalRoute53 = func(context.Context, Route53API) ([]HostedZone, error) {
		return nil, nil
	}
	listGlobalS3 = func(context.Context, S3API) ([]S3Bucket, error) {
		return nil, nil
	}

	var mu sync.Mutex
	var messages []string
	_, err := Scan(context.Background(), Query{
		Targets:      []AccountTarget{{AccountID: "111111111111", DisplayName: "solo"}},
		Workers:      1,
		regionLister: stubRegionLister{regions: []string{"us-east-1", "us-west-2", "eu-west-1"}},
		OnProgress: func(msg string) {
			mu.Lock()
			messages = append(messages, msg)
			mu.Unlock()
		},
	})
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	joined := strings.Join(messages, "\n")
	for _, want := range []string{
		"Scanning inventory for solo (111111111111) (3 regions)",
		"1/3 regions",
		"3/3 regions",
		"Route53 and S3",
		"Scanned inventory for solo (111111111111)",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing %q in progress:\n%s", want, joined)
		}
	}
}

func TestShouldReportRegionProgress(t *testing.T) {
	t.Parallel()
	cases := []struct {
		done, total int
		want        bool
	}{
		{1, 34, true},
		{5, 34, true},
		{24, 34, false},
		{25, 34, true},
		{30, 34, true},
		{34, 34, true},
		{2, 10, true},
	}
	for _, tc := range cases {
		if got := shouldReportRegionProgress(tc.done, tc.total); got != tc.want {
			t.Fatalf("shouldReportRegionProgress(%d, %d) = %v, want %v", tc.done, tc.total, got, tc.want)
		}
	}
}

func TestScanRegionTimeoutRecordsWarning(t *testing.T) {
	origTimeout := regionScanTimeout
	origEC2 := listRegionalEC2
	origRDS := listRegionalRDS
	origLBs := listRegionalLBs
	origLambda := listRegionalLambda
	origR53 := listGlobalRoute53
	origS3 := listGlobalS3
	t.Cleanup(func() {
		regionScanTimeout = origTimeout
		listRegionalEC2 = origEC2
		listRegionalRDS = origRDS
		listRegionalLBs = origLBs
		listRegionalLambda = origLambda
		listGlobalRoute53 = origR53
		listGlobalS3 = origS3
	})
	regionScanTimeout = 30 * time.Millisecond
	listRegionalEC2 = func(ctx context.Context, _ EC2API, _ string) ([]EC2Instance, []EBSVolume, []ElasticIP, []NATGateway, []VPC, error) {
		<-ctx.Done()
		return nil, nil, nil, nil, nil, ctx.Err()
	}
	rdsCalled := false
	listRegionalRDS = func(context.Context, RDSAPI, string) ([]RDSInstance, []RDSCluster, error) {
		rdsCalled = true
		return nil, nil, nil
	}
	listRegionalLBs = func(context.Context, ELBV2API, ELBAPI, string) ([]LoadBalancer, error) {
		return nil, nil
	}
	listRegionalLambda = func(context.Context, LambdaAPI, string) ([]LambdaFunction, error) {
		return nil, nil
	}
	listGlobalRoute53 = func(context.Context, Route53API) ([]HostedZone, error) {
		return nil, nil
	}
	listGlobalS3 = func(context.Context, S3API) ([]S3Bucket, error) {
		return nil, nil
	}

	result, err := Scan(context.Background(), Query{
		Targets:      []AccountTarget{{AccountID: "111111111111"}},
		Workers:      1,
		regionLister: stubRegionLister{regions: []string{"ap-southeast-4"}},
	})
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	if len(result.Accounts) != 1 {
		t.Fatalf("accounts = %d", len(result.Accounts))
	}
	if rdsCalled {
		t.Fatal("RDS should not run after the region context is done")
	}
	skipped := result.Accounts[0].SkippedRegions
	if len(skipped) != 1 {
		t.Fatalf("skipped regions = %+v, want a single timeout warning", skipped)
	}
	if skipped[0].Region != "ap-southeast-4" || !strings.Contains(skipped[0].Message, "timed out") {
		t.Fatalf("timeout warning = %+v", skipped[0])
	}
	if strings.Contains(skipped[0].Message, "ec2:") || strings.Contains(skipped[0].Message, "deadline") {
		t.Fatalf("should not record per-service context errors: %+v", skipped[0])
	}
}

func TestScanGlobalTimeoutRecordsWarning(t *testing.T) {
	origTimeout := globalScanTimeout
	origEC2 := listRegionalEC2
	origRDS := listRegionalRDS
	origLBs := listRegionalLBs
	origLambda := listRegionalLambda
	origR53 := listGlobalRoute53
	origS3 := listGlobalS3
	t.Cleanup(func() {
		globalScanTimeout = origTimeout
		listRegionalEC2 = origEC2
		listRegionalRDS = origRDS
		listRegionalLBs = origLBs
		listRegionalLambda = origLambda
		listGlobalRoute53 = origR53
		listGlobalS3 = origS3
	})
	globalScanTimeout = 30 * time.Millisecond
	listRegionalEC2 = func(context.Context, EC2API, string) ([]EC2Instance, []EBSVolume, []ElasticIP, []NATGateway, []VPC, error) {
		return nil, nil, nil, nil, nil, nil
	}
	listRegionalRDS = func(context.Context, RDSAPI, string) ([]RDSInstance, []RDSCluster, error) {
		return nil, nil, nil
	}
	listRegionalLBs = func(context.Context, ELBV2API, ELBAPI, string) ([]LoadBalancer, error) {
		return nil, nil
	}
	listRegionalLambda = func(context.Context, LambdaAPI, string) ([]LambdaFunction, error) {
		return nil, nil
	}
	listGlobalRoute53 = func(ctx context.Context, _ Route53API) ([]HostedZone, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	s3Called := false
	listGlobalS3 = func(context.Context, S3API) ([]S3Bucket, error) {
		s3Called = true
		return nil, nil
	}

	result, err := Scan(context.Background(), Query{
		Targets:      []AccountTarget{{AccountID: "111111111111"}},
		Workers:      1,
		regionLister: stubRegionLister{regions: []string{"us-east-1"}},
	})
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	if len(result.Accounts) != 1 {
		t.Fatalf("accounts = %d", len(result.Accounts))
	}
	if s3Called {
		t.Fatal("S3 should not run after the global context is done")
	}
	warnings := result.Accounts[0].Warnings
	if len(warnings) != 1 || !strings.Contains(warnings[0], "timed out") {
		t.Fatalf("warnings = %+v, want a single timeout", warnings)
	}
	if strings.Contains(warnings[0], "route53:") {
		t.Fatalf("should not record per-service context errors: %+v", warnings)
	}
}

func TestWithInventoryAPILimitsPreservesCustomHTTPClient(t *testing.T) {
	t.Parallel()
	custom := &http.Client{Timeout: 3 * time.Second}
	cfg := aws.Config{HTTPClient: custom, RetryMaxAttempts: 10}
	got := withInventoryAPILimits(cfg)
	if got.HTTPClient != custom {
		t.Fatal("custom HTTPClient was replaced")
	}
	if got.RetryMaxAttempts != 2 {
		t.Fatalf("RetryMaxAttempts = %d, want 2", got.RetryMaxAttempts)
	}
	if cfg.RetryMaxAttempts != 10 || cfg.HTTPClient != custom {
		t.Fatal("original config mutated")
	}
}

func TestWithInventoryAPILimitsWrapsBuildableClient(t *testing.T) {
	t.Parallel()
	base := awshttp.NewBuildableClient()
	got := withInventoryAPILimits(aws.Config{HTTPClient: base})
	if got.HTTPClient == base {
		t.Fatal("expected a timeout clone, not the same pointer")
	}
	wrapped, ok := got.HTTPClient.(*awshttp.BuildableClient)
	if !ok {
		t.Fatalf("HTTPClient type %T, want *awshttp.BuildableClient", got.HTTPClient)
	}
	if wrapped.GetTimeout() != inventoryHTTPTimeout {
		t.Fatalf("timeout = %s, want %s", wrapped.GetTimeout(), inventoryHTTPTimeout)
	}
}

func TestWithInventoryAPILimitsNilClientUsesBuildableClient(t *testing.T) {
	t.Parallel()
	got := withInventoryAPILimits(aws.Config{})
	wrapped, ok := got.HTTPClient.(*awshttp.BuildableClient)
	if !ok {
		t.Fatalf("HTTPClient type %T, want *awshttp.BuildableClient", got.HTTPClient)
	}
	if wrapped.GetTimeout() != inventoryHTTPTimeout {
		t.Fatalf("timeout = %s, want %s", wrapped.GetTimeout(), inventoryHTTPTimeout)
	}
}

func TestAwsConfigForRegionOnlySetsRegion(t *testing.T) {
	t.Parallel()
	custom := &http.Client{Timeout: 3 * time.Second}
	cfg := aws.Config{HTTPClient: custom, RetryMaxAttempts: 2, Region: "us-east-1"}
	got := awsConfigForRegion(cfg, "eu-west-1")
	if got.Region != "eu-west-1" {
		t.Fatalf("region = %q", got.Region)
	}
	if got.HTTPClient != custom {
		t.Fatal("HTTPClient replaced")
	}
	if got.RetryMaxAttempts != 2 {
		t.Fatalf("RetryMaxAttempts = %d", got.RetryMaxAttempts)
	}
	if cfg.Region != "us-east-1" {
		t.Fatal("original config mutated")
	}
}

type fakeLambdaAPI struct {
	pages   [][]string
	pageErr error
}

func (f *fakeLambdaAPI) ListFunctions(
	_ context.Context,
	params *lambda.ListFunctionsInput,
	_ ...func(*lambda.Options),
) (*lambda.ListFunctionsOutput, error) {
	idx := 0
	if params != nil && params.Marker != nil {
		idx = 1
	}
	if f.pageErr != nil && idx > 0 {
		return nil, f.pageErr
	}
	if idx >= len(f.pages) {
		return &lambda.ListFunctionsOutput{}, nil
	}
	out := &lambda.ListFunctionsOutput{}
	for _, name := range f.pages[idx] {
		n := name
		out.Functions = append(out.Functions, lambdatypes.FunctionConfiguration{FunctionName: &n})
	}
	if idx+1 < len(f.pages) || f.pageErr != nil {
		marker := "next"
		out.NextMarker = &marker
	}
	return out, nil
}

func TestListLambdaFunctionsPaginatesPastFifty(t *testing.T) {
	t.Parallel()
	page1 := make([]string, 50)
	for i := range page1 {
		page1[i] = fmt.Sprintf("fn-%02d", i)
	}
	fake := &fakeLambdaAPI{pages: [][]string{page1, {"fn-50", "fn-51"}}}
	got, err := listLambdaFunctions(context.Background(), fake, "us-east-1")
	if err != nil {
		t.Fatalf("listLambdaFunctions() error = %v", err)
	}
	if len(got) != 52 {
		t.Fatalf("got %d functions, want 52", len(got))
	}
}

func TestListLambdaFunctionsKeepsPageWhenLaterPageFails(t *testing.T) {
	t.Parallel()
	fake := &fakeLambdaAPI{
		pages:   [][]string{{"fn-0", "fn-1"}},
		pageErr: fmt.Errorf("throttled"),
	}
	got, err := listLambdaFunctions(context.Background(), fake, "us-east-1")
	if err == nil {
		t.Fatal("expected paging error")
	}
	if len(got) != 2 || got[0].Name != "fn-0" || got[1].Name != "fn-1" {
		t.Fatalf("got %+v, want first page", got)
	}
}

func TestSortInventoryOrdersAllSlices(t *testing.T) {
	t.Parallel()
	inv := AccountInventory{
		EC2Instances:    []EC2Instance{{InstanceID: "i-2"}, {InstanceID: "i-1"}},
		RDSInstances:    []RDSInstance{{InstanceID: "db-b"}, {InstanceID: "db-a"}},
		RDSClusters:     []RDSCluster{{ClusterID: "cl-b"}, {ClusterID: "cl-a"}},
		HostedZones:     []HostedZone{{Name: "z.example"}, {Name: "a.example"}},
		UnattachedEBS:   []EBSVolume{{VolumeID: "vol-2"}, {VolumeID: "vol-1"}},
		ElasticIPs:      []ElasticIP{{PublicIP: "2.2.2.2"}, {PublicIP: "1.1.1.1"}},
		LoadBalancers:   []LoadBalancer{{Name: "lb-b"}, {Name: "lb-a"}},
		NATGateways:     []NATGateway{{GatewayID: "nat-2"}, {GatewayID: "nat-1"}},
		S3Buckets:       []S3Bucket{{Name: "bucket-b"}, {Name: "bucket-a"}},
		LambdaFunctions: []LambdaFunction{{Name: "fn-b"}, {Name: "fn-a"}},
		VPCs:            []VPC{{VPCID: "vpc-2"}, {VPCID: "vpc-1"}},
	}
	sortInventory(&inv)
	if inv.EC2Instances[0].InstanceID != "i-1" || inv.RDSInstances[0].InstanceID != "db-a" ||
		inv.RDSClusters[0].ClusterID != "cl-a" || inv.HostedZones[0].Name != "a.example" ||
		inv.UnattachedEBS[0].VolumeID != "vol-1" || inv.ElasticIPs[0].PublicIP != "1.1.1.1" ||
		inv.LoadBalancers[0].Name != "lb-a" || inv.NATGateways[0].GatewayID != "nat-1" ||
		inv.S3Buckets[0].Name != "bucket-a" || inv.LambdaFunctions[0].Name != "fn-a" ||
		inv.VPCs[0].VPCID != "vpc-1" {
		t.Fatalf("sortInventory() did not order all slices: %+v", inv)
	}
}

func TestScanRegionalResourcesKeepsRDSWhenEC2Fails(t *testing.T) {
	origEC2 := listRegionalEC2
	origRDS := listRegionalRDS
	origLBs := listRegionalLBs
	origLambda := listRegionalLambda
	t.Cleanup(func() {
		listRegionalEC2 = origEC2
		listRegionalRDS = origRDS
		listRegionalLBs = origLBs
		listRegionalLambda = origLambda
	})

	listRegionalEC2 = func(context.Context, EC2API, string) ([]EC2Instance, []EBSVolume, []ElasticIP, []NATGateway, []VPC, error) {
		return nil, nil, nil, nil, nil, fmt.Errorf("access denied")
	}
	listRegionalRDS = func(context.Context, RDSAPI, string) ([]RDSInstance, []RDSCluster, error) {
		return []RDSInstance{{InstanceID: "db-1", Region: "us-east-1"}}, nil, nil
	}
	listRegionalLBs = func(context.Context, ELBV2API, ELBAPI, string) ([]LoadBalancer, error) {
		return nil, nil
	}
	listRegionalLambda = func(context.Context, LambdaAPI, string) ([]LambdaFunction, error) {
		return nil, nil
	}

	inv := &AccountInventory{}
	var mu sync.Mutex
	var warnings []RegionWarning
	scanRegionalResources(context.Background(), aws.Config{}, "us-east-1", "111111111111", inv, &mu, &warnings)
	if len(inv.RDSInstances) != 1 || inv.RDSInstances[0].InstanceID != "db-1" {
		t.Fatalf("rds = %+v", inv.RDSInstances)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0].Message, "ec2") {
		t.Fatalf("warnings = %+v", warnings)
	}
}

func TestScanRegionalResourcesKeepsPartialEC2OnError(t *testing.T) {
	origEC2 := listRegionalEC2
	origRDS := listRegionalRDS
	origLBs := listRegionalLBs
	origLambda := listRegionalLambda
	t.Cleanup(func() {
		listRegionalEC2 = origEC2
		listRegionalRDS = origRDS
		listRegionalLBs = origLBs
		listRegionalLambda = origLambda
	})

	listRegionalEC2 = func(context.Context, EC2API, string) ([]EC2Instance, []EBSVolume, []ElasticIP, []NATGateway, []VPC, error) {
		return []EC2Instance{{InstanceID: "i-1", Region: "us-east-1"}},
			[]EBSVolume{{VolumeID: "vol-1", Region: "us-east-1"}},
			nil, nil, nil, fmt.Errorf("nat-gateways: access denied")
	}
	listRegionalRDS = func(context.Context, RDSAPI, string) ([]RDSInstance, []RDSCluster, error) {
		return nil, nil, nil
	}
	listRegionalLBs = func(context.Context, ELBV2API, ELBAPI, string) ([]LoadBalancer, error) {
		return nil, nil
	}
	listRegionalLambda = func(context.Context, LambdaAPI, string) ([]LambdaFunction, error) {
		return nil, nil
	}

	inv := &AccountInventory{}
	var mu sync.Mutex
	var warnings []RegionWarning
	scanRegionalResources(context.Background(), aws.Config{}, "us-east-1", "111111111111", inv, &mu, &warnings)
	if len(inv.EC2Instances) != 1 || inv.EC2Instances[0].InstanceID != "i-1" {
		t.Fatalf("ec2 = %+v", inv.EC2Instances)
	}
	if len(inv.UnattachedEBS) != 1 || inv.UnattachedEBS[0].VolumeID != "vol-1" {
		t.Fatalf("volumes = %+v", inv.UnattachedEBS)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0].Message, "nat-gateways") {
		t.Fatalf("warnings = %+v", warnings)
	}
}

func TestScanRegionalResourcesKeepsPartialLambdaOnError(t *testing.T) {
	origEC2 := listRegionalEC2
	origRDS := listRegionalRDS
	origLBs := listRegionalLBs
	origLambda := listRegionalLambda
	t.Cleanup(func() {
		listRegionalEC2 = origEC2
		listRegionalRDS = origRDS
		listRegionalLBs = origLBs
		listRegionalLambda = origLambda
	})

	listRegionalEC2 = func(context.Context, EC2API, string) ([]EC2Instance, []EBSVolume, []ElasticIP, []NATGateway, []VPC, error) {
		return nil, nil, nil, nil, nil, nil
	}
	listRegionalRDS = func(context.Context, RDSAPI, string) ([]RDSInstance, []RDSCluster, error) {
		return nil, nil, nil
	}
	listRegionalLBs = func(context.Context, ELBV2API, ELBAPI, string) ([]LoadBalancer, error) {
		return nil, nil
	}
	listRegionalLambda = func(context.Context, LambdaAPI, string) ([]LambdaFunction, error) {
		return []LambdaFunction{{Name: "fn-1", Region: "us-east-1"}}, fmt.Errorf("lambda paging failed")
	}

	inv := &AccountInventory{}
	var mu sync.Mutex
	var warnings []RegionWarning
	scanRegionalResources(context.Background(), aws.Config{}, "us-east-1", "111111111111", inv, &mu, &warnings)
	if len(inv.LambdaFunctions) != 1 || inv.LambdaFunctions[0].Name != "fn-1" {
		t.Fatalf("lambda = %+v", inv.LambdaFunctions)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0].Message, "lambda paging failed") {
		t.Fatalf("warnings = %+v", warnings)
	}
}

func TestScanGlobalResourcesKeepsBucketsWhenLocationFails(t *testing.T) {
	origR53 := listGlobalRoute53
	origS3 := listGlobalS3
	t.Cleanup(func() {
		listGlobalRoute53 = origR53
		listGlobalS3 = origS3
	})

	listGlobalRoute53 = func(context.Context, Route53API) ([]HostedZone, error) {
		return nil, nil
	}
	listGlobalS3 = func(context.Context, S3API) ([]S3Bucket, error) {
		return []S3Bucket{{Name: "kept", Region: ""}}, fmt.Errorf("kept: access denied")
	}

	inv := &AccountInventory{}
	scanGlobalResources(context.Background(), aws.Config{}, inv)
	if len(inv.S3Buckets) != 1 || inv.S3Buckets[0].Name != "kept" || inv.S3Buckets[0].Region != "" {
		t.Fatalf("s3 = %+v", inv.S3Buckets)
	}
	if len(inv.Warnings) != 1 || !strings.Contains(inv.Warnings[0], "s3:") {
		t.Fatalf("warnings = %+v", inv.Warnings)
	}
}

func TestScanGlobalResourcesKeepsZonesWhenLaterPageFails(t *testing.T) {
	origR53 := listGlobalRoute53
	origS3 := listGlobalS3
	t.Cleanup(func() {
		listGlobalRoute53 = origR53
		listGlobalS3 = origS3
	})

	listGlobalRoute53 = func(context.Context, Route53API) ([]HostedZone, error) {
		return []HostedZone{{Name: "kept.example.", ZoneID: "/hostedzone/Z1"}}, fmt.Errorf("throttled")
	}
	listGlobalS3 = func(context.Context, S3API) ([]S3Bucket, error) {
		return nil, nil
	}

	inv := &AccountInventory{}
	scanGlobalResources(context.Background(), aws.Config{}, inv)
	if len(inv.HostedZones) != 1 || inv.HostedZones[0].Name != "kept.example." {
		t.Fatalf("zones = %+v", inv.HostedZones)
	}
	if len(inv.Warnings) != 1 || !strings.Contains(inv.Warnings[0], "route53:") {
		t.Fatalf("warnings = %+v", inv.Warnings)
	}
}

func TestScanGlobalResourcesRecordsHTTPDeadlineWhileScanCtxAlive(t *testing.T) {
	origR53 := listGlobalRoute53
	origS3 := listGlobalS3
	t.Cleanup(func() {
		listGlobalRoute53 = origR53
		listGlobalS3 = origS3
	})

	listGlobalRoute53 = func(context.Context, Route53API) ([]HostedZone, error) {
		// Simulate HTTP-client timeout: DeadlineExceeded while enclosing scan ctx is still active.
		return nil, context.DeadlineExceeded
	}
	listGlobalS3 = func(context.Context, S3API) ([]S3Bucket, error) {
		return nil, nil
	}

	inv := &AccountInventory{}
	scanGlobalResources(context.Background(), aws.Config{}, inv)
	if len(inv.Warnings) != 1 || !strings.Contains(inv.Warnings[0], "route53:") {
		t.Fatalf("warnings = %+v, want route53 HTTP timeout warning", inv.Warnings)
	}
}

func TestScanRegionalResourcesRecordsHTTPDeadlineWhileScanCtxAlive(t *testing.T) {
	origEC2 := listRegionalEC2
	origRDS := listRegionalRDS
	origLBs := listRegionalLBs
	origLambda := listRegionalLambda
	t.Cleanup(func() {
		listRegionalEC2 = origEC2
		listRegionalRDS = origRDS
		listRegionalLBs = origLBs
		listRegionalLambda = origLambda
	})

	listRegionalEC2 = func(context.Context, EC2API, string) ([]EC2Instance, []EBSVolume, []ElasticIP, []NATGateway, []VPC, error) {
		return nil, nil, nil, nil, nil, context.DeadlineExceeded
	}
	listRegionalRDS = func(context.Context, RDSAPI, string) ([]RDSInstance, []RDSCluster, error) {
		return nil, nil, nil
	}
	listRegionalLBs = func(context.Context, ELBV2API, ELBAPI, string) ([]LoadBalancer, error) {
		return nil, nil
	}
	listRegionalLambda = func(context.Context, LambdaAPI, string) ([]LambdaFunction, error) {
		return nil, nil
	}

	inv := &AccountInventory{}
	var mu sync.Mutex
	var warnings []RegionWarning
	scanRegionalResources(context.Background(), aws.Config{}, "us-east-1", "111111111111", inv, &mu, &warnings)
	if len(warnings) != 1 || !strings.Contains(warnings[0].Message, "ec2:") {
		t.Fatalf("warnings = %+v, want ec2 HTTP timeout warning", warnings)
	}
}

func TestShouldSuppressServiceWarning(t *testing.T) {
	t.Parallel()
	alive := context.Background()
	if shouldSuppressServiceWarning(alive, context.DeadlineExceeded) {
		t.Fatal("must not suppress HTTP-style deadline while scan ctx is alive")
	}
	if shouldSuppressServiceWarning(alive, fmt.Errorf("access denied")) {
		t.Fatal("must not suppress non-context errors")
	}

	done, cancel := context.WithCancel(context.Background())
	cancel()
	if !shouldSuppressServiceWarning(done, context.Canceled) {
		t.Fatal("must suppress when enclosing ctx is done")
	}
	if shouldSuppressServiceWarning(done, fmt.Errorf("access denied")) {
		t.Fatal("must not suppress non-context errors even when ctx is done")
	}
}
