package extendedsupport

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/rds"
	rdstypes "github.com/aws/aws-sdk-go-v2/service/rds/types"
)

func TestInExtendedSupport(t *testing.T) {
	end := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		name string
		now  time.Time
		end  time.Time
		want bool
	}{
		{name: "zero end date", now: end.Add(24 * time.Hour), end: time.Time{}, want: false},
		{name: "before end", now: end.Add(-time.Second), end: end, want: false},
		{name: "at end", now: end, end: end, want: false},
		{name: "after end", now: end.Add(time.Second), end: end, want: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := inExtendedSupport(tc.now, tc.end); got != tc.want {
				t.Fatalf("inExtendedSupport() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestListRDSExtendedSupportWithClient(t *testing.T) {
	now := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
	pastEnd := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	futureEnd := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	extendedEnd := time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)

	client := &fakeRDSClient{
		instances: []rdstypes.DBInstance{
			{
				DBInstanceIdentifier:   aws.String("db-old"),
				Engine:                 aws.String("postgres"),
				EngineVersion:          aws.String("15.4"),
				EngineLifecycleSupport: aws.String("open-source-rds-extended-support"),
				TagList: []rdstypes.Tag{
					{Key: aws.String("owner"), Value: aws.String("jdoe")},
				},
			},
			{
				DBInstanceIdentifier: aws.String("db-new"),
				Engine:               aws.String("postgres"),
				EngineVersion:        aws.String("16.4"),
			},
		},
		clusters: []rdstypes.DBCluster{
			{
				DBClusterIdentifier:    aws.String("aurora-old"),
				Engine:                 aws.String("aurora-mysql"),
				EngineVersion:          aws.String("8.0.mysql_aurora.3.04.0"),
				EngineLifecycleSupport: aws.String("open-source-rds-extended-support"),
			},
			{
				DBClusterIdentifier: aws.String("aurora-new"),
				Engine:              aws.String("aurora-postgresql"),
				EngineVersion:       aws.String("16.2"),
			},
		},
		majorByVersion: map[string]string{
			"postgres|15.4":                        "15",
			"postgres|16.4":                        "16",
			"aurora-mysql|8.0.mysql_aurora.3.04.0": "8.0",
			"aurora-postgresql|16.2":               "16",
		},
		majorVersionsByEngine: map[string][]rdstypes.DBMajorEngineVersion{
			"postgres": {
				{
					Engine:             aws.String("postgres"),
					MajorEngineVersion: aws.String("15"),
					SupportedEngineLifecycles: []rdstypes.SupportedEngineLifecycle{
						{
							LifecycleSupportName:    rdstypes.LifecycleSupportNameOpenSourceRdsStandardSupport,
							LifecycleSupportEndDate: aws.Time(pastEnd),
						},
						{
							LifecycleSupportName:    rdstypes.LifecycleSupportNameOpenSourceRdsExtendedSupport,
							LifecycleSupportEndDate: aws.Time(extendedEnd),
						},
					},
				},
				{
					Engine:             aws.String("postgres"),
					MajorEngineVersion: aws.String("16"),
					SupportedEngineLifecycles: []rdstypes.SupportedEngineLifecycle{
						{
							LifecycleSupportName:    rdstypes.LifecycleSupportNameOpenSourceRdsStandardSupport,
							LifecycleSupportEndDate: aws.Time(futureEnd),
						},
					},
				},
			},
			"aurora-mysql": {
				{
					Engine:             aws.String("aurora-mysql"),
					MajorEngineVersion: aws.String("8.0"),
					SupportedEngineLifecycles: []rdstypes.SupportedEngineLifecycle{
						{
							LifecycleSupportName:    rdstypes.LifecycleSupportNameOpenSourceRdsStandardSupport,
							LifecycleSupportEndDate: aws.Time(pastEnd),
						},
					},
				},
			},
			"aurora-postgresql": {
				{
					Engine:             aws.String("aurora-postgresql"),
					MajorEngineVersion: aws.String("16"),
					SupportedEngineLifecycles: []rdstypes.SupportedEngineLifecycle{
						{
							LifecycleSupportName:    rdstypes.LifecycleSupportNameOpenSourceRdsStandardSupport,
							LifecycleSupportEndDate: aws.Time(futureEnd),
						},
					},
				},
			},
		},
	}

	result, err := listRDSExtendedSupportWithClient(
		context.Background(), client, "111111111111", "us-east-1", now)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Records) != 2 {
		t.Fatalf("records = %d, want 2 (only resources past standard support end)", len(result.Records))
	}

	first := result.Records[0]
	if first.ResourceID != "aurora-old" || first.ResourceType != ResourceTypeRDSCluster {
		t.Fatalf("first record = %+v", first)
	}
	if !first.StandardSupportEndDate.Equal(pastEnd) {
		t.Fatalf("standard support end = %v, want %v", first.StandardSupportEndDate, pastEnd)
	}
	if first.ExtendedSupportEndDate != nil {
		t.Fatalf("extended support end = %v, want nil", first.ExtendedSupportEndDate)
	}

	second := result.Records[1]
	if second.ResourceID != "db-old" || second.ResourceType != ResourceTypeRDSInstance {
		t.Fatalf("second record = %+v", second)
	}
	if !second.StandardSupportEndDate.Equal(pastEnd) {
		t.Fatalf("standard support end = %v, want %v", second.StandardSupportEndDate, pastEnd)
	}
	if second.ExtendedSupportEndDate == nil || !second.ExtendedSupportEndDate.Equal(extendedEnd) {
		t.Fatalf("extended support end = %v, want %v", second.ExtendedSupportEndDate, extendedEnd)
	}
	if second.EngineLifecycleSupport != "open-source-rds-extended-support" {
		t.Fatalf("engine lifecycle support = %q", second.EngineLifecycleSupport)
	}
	if second.Tags["owner"] != "jdoe" {
		t.Fatalf("tags = %v", second.Tags)
	}
}

func TestListRDSExtendedSupportSurfacesInstanceError(t *testing.T) {
	client := &fakeRDSClient{
		instanceErr: fmt.Errorf("could not connect to the endpoint"),
	}
	result, err := listRDSExtendedSupportWithClient(
		context.Background(), client, "111111111111", "us-east-1", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Warnings) != 1 || !strings.Contains(result.Warnings[0].Message, "could not connect") {
		t.Fatalf("warnings = %+v", result.Warnings)
	}
}

func TestListRDSExtendedSupportSkipsRegionWithoutRDS(t *testing.T) {
	client := &fakeRDSClient{
		instanceErr: fmt.Errorf("InvalidParameterValue: The API for service 'rds' is not available in this region"),
	}
	result, err := listRDSExtendedSupportWithClient(
		context.Background(), client, "111111111111", "ap-east-1", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Records) != 0 {
		t.Fatalf("records = %d, want 0", len(result.Records))
	}
}

func TestListRDSExtendedSupportMissingEngineVersion(t *testing.T) {
	client := &fakeRDSClient{
		instances: []rdstypes.DBInstance{{
			DBInstanceIdentifier: aws.String("db-unknown"),
			Engine:               aws.String("mysql"),
			EngineVersion:        aws.String("8.0.99"),
		}},
		majorByVersion:        map[string]string{},
		majorVersionsByEngine: map[string][]rdstypes.DBMajorEngineVersion{},
	}
	result, err := listRDSExtendedSupportWithClient(
		context.Background(), client, "111111111111", "us-east-1", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.UnclassifiedResources) != 1 || result.UnclassifiedResources[0].ResourceID != "db-unknown" {
		t.Fatalf("unclassified = %+v", result.UnclassifiedResources)
	}
}

type fakeRDSClient struct {
	instances             []rdstypes.DBInstance
	clusters              []rdstypes.DBCluster
	instanceErr           error
	clusterErr            error
	versionErrors         map[string]error
	lifecycleErrors       map[string]error
	engineVersionInputs   []*rds.DescribeDBEngineVersionsInput
	requireIncludeAll     bool
	instancePages         []*rds.DescribeDBInstancesOutput
	instanceCalls         int
	lifecyclePages        map[string][]*rds.DescribeDBMajorEngineVersionsOutput
	lifecycleCalls        map[string]int
	majorByVersion        map[string]string
	majorVersionsByEngine map[string][]rdstypes.DBMajorEngineVersion
}

func (c *fakeRDSClient) DescribeDBInstances(
	_ context.Context,
	_ *rds.DescribeDBInstancesInput,
	_ ...func(*rds.Options),
) (*rds.DescribeDBInstancesOutput, error) {
	c.instanceCalls++
	if c.instanceCalls <= len(c.instancePages) {
		return c.instancePages[c.instanceCalls-1], nil
	}
	if c.instanceErr != nil {
		return nil, c.instanceErr
	}
	return &rds.DescribeDBInstancesOutput{DBInstances: c.instances}, nil
}

func (c *fakeRDSClient) DescribeDBClusters(
	_ context.Context,
	_ *rds.DescribeDBClustersInput,
	_ ...func(*rds.Options),
) (*rds.DescribeDBClustersOutput, error) {
	if c.clusterErr != nil {
		return nil, c.clusterErr
	}
	return &rds.DescribeDBClustersOutput{DBClusters: c.clusters}, nil
}

func (c *fakeRDSClient) DescribeDBEngineVersions(
	_ context.Context,
	params *rds.DescribeDBEngineVersionsInput,
	_ ...func(*rds.Options),
) (*rds.DescribeDBEngineVersionsOutput, error) {
	c.engineVersionInputs = append(c.engineVersionInputs, params)
	if c.requireIncludeAll && !aws.ToBool(params.IncludeAll) {
		return &rds.DescribeDBEngineVersionsOutput{}, nil
	}
	key := fmt.Sprintf("%s|%s", aws.ToString(params.Engine), aws.ToString(params.EngineVersion))
	if err := c.versionErrors[key]; err != nil {
		return nil, err
	}
	major, ok := c.majorByVersion[key]
	if !ok {
		return &rds.DescribeDBEngineVersionsOutput{}, nil
	}
	return &rds.DescribeDBEngineVersionsOutput{
		DBEngineVersions: []rdstypes.DBEngineVersion{{
			Engine:             params.Engine,
			EngineVersion:      params.EngineVersion,
			MajorEngineVersion: aws.String(major),
		}},
	}, nil
}

func classificationClient() *fakeRDSClient {
	end := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	return &fakeRDSClient{
		instances:      []rdstypes.DBInstance{{DBInstanceIdentifier: aws.String("db-known"), Engine: aws.String("postgres"), EngineVersion: aws.String("11.1")}},
		majorByVersion: map[string]string{"postgres|11.1": "11"},
		majorVersionsByEngine: map[string][]rdstypes.DBMajorEngineVersion{
			"postgres": {{Engine: aws.String("postgres"), MajorEngineVersion: aws.String("11"), SupportedEngineLifecycles: []rdstypes.SupportedEngineLifecycle{{LifecycleSupportName: rdstypes.LifecycleSupportNameOpenSourceRdsStandardSupport, LifecycleSupportEndDate: &end}}}},
		},
	}
}

func TestListRDSExtendedSupportIncludesDeprecatedVersions(t *testing.T) {
	client := classificationClient()
	client.requireIncludeAll = true
	client.clusters = []rdstypes.DBCluster{{DBClusterIdentifier: aws.String("cluster-known"), Engine: aws.String("postgres"), EngineVersion: aws.String("11.1")}}
	result, err := listRDSExtendedSupportWithClient(context.Background(), client, "111111111111", "us-east-1", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Records) != 2 || len(result.UnclassifiedResources) != 0 {
		t.Fatalf("result = %+v", result)
	}
	if len(client.engineVersionInputs) != 1 {
		t.Fatalf("lookups = %d, want one per distinct version", len(client.engineVersionInputs))
	}
}

func TestListRDSExtendedSupportRetainsRecordsWhenVersionLookupFails(t *testing.T) {
	for _, failure := range []error{nil, errors.New("lookup denied")} {
		t.Run(fmt.Sprint(failure), func(t *testing.T) {
			client := classificationClient()
			client.instances = append(client.instances, rdstypes.DBInstance{DBInstanceIdentifier: aws.String("db-unknown"), Engine: aws.String("postgres"), EngineVersion: aws.String("11.2")})
			client.versionErrors = map[string]error{"postgres|11.2": failure}
			result, err := listRDSExtendedSupportWithClient(context.Background(), client, "111111111111", "us-east-1", time.Now())
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Records) != 1 || result.Records[0].ResourceID != "db-known" {
				t.Fatalf("records = %+v", result.Records)
			}
			if len(result.UnclassifiedResources) != 1 || result.UnclassifiedResources[0].ResourceID != "db-unknown" {
				t.Fatalf("unclassified = %+v", result.UnclassifiedResources)
			}
			if len(result.Warnings) != 0 {
				t.Fatalf("whole region should not be skipped: %+v", result.Warnings)
			}
		})
	}
}

func TestListRDSExtendedSupportRetainsOtherEnginesWhenLifecycleLookupFails(t *testing.T) {
	client := classificationClient()
	client.clusters = []rdstypes.DBCluster{{DBClusterIdentifier: aws.String("cluster-unknown"), Engine: aws.String("aurora-mysql"), EngineVersion: aws.String("5.7.mysql_aurora.2.11.0")}}
	client.majorByVersion["aurora-mysql|5.7.mysql_aurora.2.11.0"] = "5.7"
	client.lifecycleErrors = map[string]error{"aurora-mysql": errors.New("catalog denied")}
	result, err := listRDSExtendedSupportWithClient(context.Background(), client, "111111111111", "us-east-1", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Records) != 1 || result.Records[0].ResourceID != "db-known" {
		t.Fatalf("records = %+v", result.Records)
	}
	if len(result.UnclassifiedResources) != 1 || result.UnclassifiedResources[0].ResourceID != "cluster-unknown" || !strings.Contains(result.UnclassifiedResources[0].Message, "catalog denied") {
		t.Fatalf("unclassified = %+v", result.UnclassifiedResources)
	}
}

func TestListRDSExtendedSupportReportsMissingSupportInformation(t *testing.T) {
	tests := []struct {
		name    string
		change  func(*fakeRDSClient)
		message string
	}{
		{"engine missing", func(c *fakeRDSClient) { c.instances[0].Engine = nil }, "database engine is missing"},
		{"version missing", func(c *fakeRDSClient) { c.instances[0].EngineVersion = nil }, "engine version is missing"},
		{"catalog missing", func(c *fakeRDSClient) { c.majorVersionsByEngine = nil }, "lifecycle information"},
		{"major missing", func(c *fakeRDSClient) { c.majorVersionsByEngine["postgres"][0].MajorEngineVersion = aws.String("12") }, "lifecycle information"},
		{"support end missing", func(c *fakeRDSClient) { c.majorVersionsByEngine["postgres"][0].SupportedEngineLifecycles = nil }, "standard support end date"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			client := classificationClient()
			tc.change(client)
			result, err := listRDSExtendedSupportWithClient(context.Background(), client, "111111111111", "us-east-1", time.Now())
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Records) != 0 || len(result.UnclassifiedResources) != 1 {
				t.Fatalf("result = %+v", result)
			}
			warning := result.UnclassifiedResources[0]
			if warning.AccountID != "111111111111" || warning.Region != "us-east-1" || warning.ResourceType != ResourceTypeRDSInstance || warning.ResourceID != "db-known" || !strings.Contains(warning.Message, tc.message) {
				t.Fatalf("warning = %+v", warning)
			}
		})
	}
}

func TestListRDSExtendedSupportRetainsOtherDiscoveryResults(t *testing.T) {
	for _, failedKind := range []string{"instance", "cluster", "both"} {
		t.Run(failedKind, func(t *testing.T) {
			client := classificationClient()
			client.clusters = []rdstypes.DBCluster{{DBClusterIdentifier: aws.String("cluster-known"), Engine: aws.String("postgres"), EngineVersion: aws.String("11.1")}}
			wantID := "db-known"
			wantRecords, wantWarnings := 1, 1
			if failedKind == "instance" || failedKind == "both" {
				client.instanceErr = errors.New("instances denied")
				wantID = "cluster-known"
			}
			if failedKind == "cluster" || failedKind == "both" {
				client.clusterErr = errors.New("clusters denied")
			}
			if failedKind == "both" {
				wantRecords, wantWarnings = 0, 2
			}
			result, err := listRDSExtendedSupportWithClient(context.Background(), client, "111111111111", "us-east-1", time.Now())
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Records) != wantRecords || len(result.Warnings) != wantWarnings {
				t.Fatalf("result = %+v", result)
			}
			if wantRecords > 0 && result.Records[0].ResourceID != wantID {
				t.Fatalf("record = %+v", result.Records[0])
			}
		})
	}
}

func TestListRDSExtendedSupportRetainsEarlierInventoryPages(t *testing.T) {
	client := classificationClient()
	client.instancePages = []*rds.DescribeDBInstancesOutput{{DBInstances: client.instances, Marker: aws.String("next")}}
	client.instanceErr = errors.New("next page denied")
	result, err := listRDSExtendedSupportWithClient(context.Background(), client, "111111111111", "us-east-1", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Records) != 1 || len(result.Warnings) != 1 || !strings.Contains(result.Warnings[0].Message, "next page denied") {
		t.Fatalf("result = %+v", result)
	}
}

func TestListRDSExtendedSupportIgnoresIneligibleEngines(t *testing.T) {
	client := &fakeRDSClient{instances: []rdstypes.DBInstance{{DBInstanceIdentifier: aws.String("sql-server"), Engine: aws.String("sqlserver-ee"), EngineVersion: aws.String("15.00")}}}
	result, err := listRDSExtendedSupportWithClient(context.Background(), client, "111111111111", "us-east-1", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Records) != 0 || len(result.UnclassifiedResources) != 0 || len(client.engineVersionInputs) != 0 {
		t.Fatalf("result = %+v, lookups = %d", result, len(client.engineVersionInputs))
	}
}

func TestListRDSExtendedSupportPropagatesCancellation(t *testing.T) {
	client := classificationClient()
	client.versionErrors = map[string]error{"postgres|11.1": context.Canceled}
	_, err := listRDSExtendedSupportWithClient(context.Background(), client, "111111111111", "us-east-1", time.Now())
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
}

func (c *fakeRDSClient) DescribeDBMajorEngineVersions(
	_ context.Context,
	params *rds.DescribeDBMajorEngineVersionsInput,
	_ ...func(*rds.Options),
) (*rds.DescribeDBMajorEngineVersionsOutput, error) {
	engine := aws.ToString(params.Engine)
	if len(c.lifecyclePages[engine]) > 0 {
		if c.lifecycleCalls == nil {
			c.lifecycleCalls = make(map[string]int)
		}
		call := c.lifecycleCalls[engine]
		c.lifecycleCalls[engine]++
		if call < len(c.lifecyclePages[engine]) {
			return c.lifecyclePages[engine][call], nil
		}
	}
	if err := c.lifecycleErrors[aws.ToString(params.Engine)]; err != nil {
		return nil, err
	}
	return &rds.DescribeDBMajorEngineVersionsOutput{
		DBMajorEngineVersions: c.majorVersionsByEngine[aws.ToString(params.Engine)],
	}, nil
}

func TestListRDSExtendedSupportRetainsEarlierLifecyclePages(t *testing.T) {
	client := classificationClient()
	client.instances = append(client.instances, rdstypes.DBInstance{DBInstanceIdentifier: aws.String("db-unknown"), Engine: aws.String("postgres"), EngineVersion: aws.String("12.1")})
	client.majorByVersion["postgres|12.1"] = "12"
	client.lifecyclePages = map[string][]*rds.DescribeDBMajorEngineVersionsOutput{
		"postgres": {{DBMajorEngineVersions: client.majorVersionsByEngine["postgres"], Marker: aws.String("next")}},
	}
	client.lifecycleErrors = map[string]error{"postgres": errors.New("next lifecycle page denied")}
	result, err := listRDSExtendedSupportWithClient(context.Background(), client, "111111111111", "us-east-1", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Records) != 1 || result.Records[0].ResourceID != "db-known" {
		t.Fatalf("records = %+v", result.Records)
	}
	if len(result.UnclassifiedResources) != 1 || result.UnclassifiedResources[0].ResourceID != "db-unknown" || !strings.Contains(result.UnclassifiedResources[0].Message, "next lifecycle page denied") {
		t.Fatalf("unclassified = %+v", result.UnclassifiedResources)
	}
}
