package inventory

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
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

type fakeLambdaAPI struct {
	pages [][]string
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
	if idx >= len(f.pages) {
		return &lambda.ListFunctionsOutput{}, nil
	}
	out := &lambda.ListFunctionsOutput{}
	for _, name := range f.pages[idx] {
		n := name
		out.Functions = append(out.Functions, lambdatypes.FunctionConfiguration{FunctionName: &n})
	}
	if idx+1 < len(f.pages) {
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
