package extendedsupport

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/rds"
	rdstypes "github.com/aws/aws-sdk-go-v2/service/rds/types"
	"github.com/openshift-online/finops-tools/core/apilog"
)

// RDSAPI is the subset of RDS used for extended-support discovery.
type RDSAPI interface {
	DescribeDBInstances(
		ctx context.Context,
		params *rds.DescribeDBInstancesInput,
		optFns ...func(*rds.Options),
	) (*rds.DescribeDBInstancesOutput, error)
	DescribeDBClusters(
		ctx context.Context,
		params *rds.DescribeDBClustersInput,
		optFns ...func(*rds.Options),
	) (*rds.DescribeDBClustersOutput, error)
	DescribeDBEngineVersions(
		ctx context.Context,
		params *rds.DescribeDBEngineVersionsInput,
		optFns ...func(*rds.Options),
	) (*rds.DescribeDBEngineVersionsOutput, error)
	DescribeDBMajorEngineVersions(
		ctx context.Context,
		params *rds.DescribeDBMajorEngineVersionsInput,
		optFns ...func(*rds.Options),
	) (*rds.DescribeDBMajorEngineVersionsOutput, error)
}

func newRDSClient(cfg aws.Config) RDSAPI {
	return apilog.WrapRDS(rds.NewFromConfig(cfg))
}

type rdsLister interface {
	ListRDSExtendedSupport(
		ctx context.Context,
		cfg aws.Config,
		region, accountID string,
		now time.Time,
	) (regionScanResult, error)
}

type awsRDSLister struct{}

func (awsRDSLister) ListRDSExtendedSupport(
	ctx context.Context,
	cfg aws.Config,
	region, accountID string,
	now time.Time,
) (regionScanResult, error) {
	regionalCfg := cfg.Copy()
	regionalCfg.Region = region
	client := newRDSClient(regionalCfg)
	return listRDSExtendedSupportWithClient(ctx, client, accountID, region, now)
}

type engineVersionKey struct {
	Engine  string
	Version string
}

type engineMajorKey struct {
	Engine string
	Major  string
}

type engineLifecycleDates struct {
	StandardSupportEnd time.Time
	ExtendedSupportEnd *time.Time
}

// listRDSExtendedSupportWithClient returns DB instances and clusters whose
// engine version is past its standard support end date in the region.
func listRDSExtendedSupportWithClient(
	ctx context.Context,
	client RDSAPI,
	accountID, region string,
	now time.Time,
) (regionScanResult, error) {
	var result regionScanResult
	instances, instanceErr := listAllDBInstances(ctx, client, region)
	if errors.Is(instanceErr, context.Canceled) {
		return result, instanceErr
	}
	clusters, clusterErr := listAllDBClusters(ctx, client, region)
	if errors.Is(clusterErr, context.Canceled) {
		return result, clusterErr
	}
	for _, failure := range []struct {
		kind string
		err  error
	}{{"DB instance", instanceErr}, {"DB cluster", clusterErr}} {
		if failure.err != nil {
			result.Warnings = append(result.Warnings, RegionWarning{
				AccountID: accountID,
				Region:    region,
				Message:   fmt.Sprintf("%s discovery incomplete: %s", failure.kind, warningMessage(failure.err)),
			})
		}
	}
	if len(instances) == 0 && len(clusters) == 0 {
		return result, nil
	}

	// Resolve each distinct engine version to its major version, then look up
	// lifecycle dates per (engine, major version) from the region's catalog.
	majorByVersion, versionErrors, err := resolveMajorEngineVersions(ctx, client, region, instances, clusters)
	if err != nil {
		return result, err
	}
	lifecycleByMajor, lifecycleErrors, err := resolveEngineLifecycleDates(ctx, client, region, majorByVersion)
	if err != nil {
		return result, err
	}

	for _, instance := range instances {
		engine := strings.TrimSpace(aws.ToString(instance.Engine))
		version := strings.TrimSpace(aws.ToString(instance.EngineVersion))
		if engine != "" && !supportsRDSExtendedSupport(engine) {
			continue
		}
		if message := unknownSupportReason(engine, version, majorByVersion, lifecycleByMajor, versionErrors, lifecycleErrors); message != "" {
			result.UnclassifiedResources = append(result.UnclassifiedResources, ResourceWarning{
				AccountID: accountID, Region: region, ResourceType: ResourceTypeRDSInstance,
				ResourceID: aws.ToString(instance.DBInstanceIdentifier), Engine: engine, EngineVersion: version, Message: message,
			})
			continue
		}
		if rec, ok := rdsInstanceRecord(instance, accountID, region, now, majorByVersion, lifecycleByMajor); ok {
			result.Records = append(result.Records, rec)
		}
	}
	for _, cluster := range clusters {
		engine := strings.TrimSpace(aws.ToString(cluster.Engine))
		version := strings.TrimSpace(aws.ToString(cluster.EngineVersion))
		if engine != "" && !supportsRDSExtendedSupport(engine) {
			continue
		}
		if message := unknownSupportReason(engine, version, majorByVersion, lifecycleByMajor, versionErrors, lifecycleErrors); message != "" {
			result.UnclassifiedResources = append(result.UnclassifiedResources, ResourceWarning{
				AccountID: accountID, Region: region, ResourceType: ResourceTypeRDSCluster,
				ResourceID: aws.ToString(cluster.DBClusterIdentifier), Engine: engine, EngineVersion: version, Message: message,
			})
			continue
		}
		if rec, ok := rdsClusterRecord(cluster, accountID, region, now, majorByVersion, lifecycleByMajor); ok {
			result.Records = append(result.Records, rec)
		}
	}
	sortRecords(result.Records)
	result.UnclassifiedResources = sortResourceWarnings(result.UnclassifiedResources)
	return result, nil
}

func supportsRDSExtendedSupport(engine string) bool {
	switch engine {
	case "mysql", "postgres", "aurora-mysql", "aurora-postgresql":
		return true
	default:
		return false
	}
}

func unknownSupportReason(
	engine, version string,
	majorByVersion map[engineVersionKey]string,
	lifecycleByMajor map[engineMajorKey]engineLifecycleDates,
	versionErrors map[engineVersionKey]error,
	lifecycleErrors map[string]error,
) string {
	if engine == "" {
		return "database engine is missing"
	}
	if version == "" {
		return "engine version is missing"
	}
	key := engineVersionKey{Engine: engine, Version: version}
	if err := versionErrors[key]; err != nil {
		return fmt.Sprintf("engine version lookup failed: %s", warningMessage(err))
	}
	major := majorByVersion[key]
	if major == "" {
		return "engine major version is missing"
	}
	dates, ok := lifecycleByMajor[engineMajorKey{Engine: engine, Major: major}]
	if !ok || dates.StandardSupportEnd.IsZero() {
		if err := lifecycleErrors[engine]; err != nil {
			return fmt.Sprintf("engine lifecycle lookup failed: %s", warningMessage(err))
		}
	}
	if !ok {
		return fmt.Sprintf("lifecycle information for engine major version %s is missing", major)
	}
	if dates.StandardSupportEnd.IsZero() {
		return fmt.Sprintf("standard support end date for engine major version %s is missing", major)
	}
	return ""
}

func resolveMajorEngineVersions(
	ctx context.Context,
	client RDSAPI,
	region string,
	instances []rdstypes.DBInstance,
	clusters []rdstypes.DBCluster,
) (map[engineVersionKey]string, map[engineVersionKey]error, error) {
	distinct := make(map[engineVersionKey]struct{})
	for _, instance := range instances {
		key := engineVersionKey{
			Engine:  strings.TrimSpace(aws.ToString(instance.Engine)),
			Version: strings.TrimSpace(aws.ToString(instance.EngineVersion)),
		}
		if supportsRDSExtendedSupport(key.Engine) && key.Version != "" {
			distinct[key] = struct{}{}
		}
	}
	for _, cluster := range clusters {
		key := engineVersionKey{
			Engine:  strings.TrimSpace(aws.ToString(cluster.Engine)),
			Version: strings.TrimSpace(aws.ToString(cluster.EngineVersion)),
		}
		if supportsRDSExtendedSupport(key.Engine) && key.Version != "" {
			distinct[key] = struct{}{}
		}
	}

	out := make(map[engineVersionKey]string, len(distinct))
	lookupErrors := make(map[engineVersionKey]error)
	for key := range distinct {
		major, err := engineMajorVersion(ctx, client, region, key.Engine, key.Version)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return nil, nil, err
			}
			lookupErrors[key] = err
			continue
		}
		out[key] = major
	}
	return out, lookupErrors, nil
}

func engineMajorVersion(ctx context.Context, client RDSAPI, region, engine, version string) (string, error) {
	var marker *string
	for {
		out, err := client.DescribeDBEngineVersions(ctx, &rds.DescribeDBEngineVersionsInput{
			Engine:        aws.String(engine),
			EngineVersion: aws.String(version),
			IncludeAll:    aws.Bool(true),
			Marker:        marker,
		})
		if err != nil {
			return "", fmt.Errorf("describe engine version %s %s in %s: %w", engine, version, region, err)
		}
		for _, engineVersion := range out.DBEngineVersions {
			if major := strings.TrimSpace(aws.ToString(engineVersion.MajorEngineVersion)); major != "" {
				return major, nil
			}
		}
		if out.Marker == nil || aws.ToString(out.Marker) == "" {
			break
		}
		marker = out.Marker
	}
	return "", fmt.Errorf("engine version %s %s not found in %s", engine, version, region)
}

func resolveEngineLifecycleDates(
	ctx context.Context,
	client RDSAPI,
	region string,
	majorByVersion map[engineVersionKey]string,
) (map[engineMajorKey]engineLifecycleDates, map[string]error, error) {
	engines := make(map[string]struct{})
	for key := range majorByVersion {
		engines[key.Engine] = struct{}{}
	}

	out := make(map[engineMajorKey]engineLifecycleDates)
	lookupErrors := make(map[string]error)
	for engine := range engines {
		majors, err := listMajorEngineVersions(ctx, client, region, engine)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return nil, nil, err
			}
			lookupErrors[engine] = err
		}
		for _, major := range majors {
			key := engineMajorKey{
				Engine: strings.TrimSpace(aws.ToString(major.Engine)),
				Major:  strings.TrimSpace(aws.ToString(major.MajorEngineVersion)),
			}
			if key.Engine == "" || key.Major == "" {
				continue
			}
			out[key] = lifecycleDates(major)
		}
	}
	return out, lookupErrors, nil
}

func listMajorEngineVersions(
	ctx context.Context,
	client RDSAPI,
	region, engine string,
) ([]rdstypes.DBMajorEngineVersion, error) {
	var out []rdstypes.DBMajorEngineVersion
	var marker *string
	for {
		page, err := client.DescribeDBMajorEngineVersions(ctx, &rds.DescribeDBMajorEngineVersionsInput{
			Engine: aws.String(engine),
			Marker: marker,
		})
		if err != nil {
			return out, fmt.Errorf("describe major engine versions for %s in %s: %w", engine, region, err)
		}
		out = append(out, page.DBMajorEngineVersions...)
		if page.Marker == nil || aws.ToString(page.Marker) == "" {
			break
		}
		marker = page.Marker
	}
	return out, nil
}

func lifecycleDates(major rdstypes.DBMajorEngineVersion) engineLifecycleDates {
	var dates engineLifecycleDates
	for _, lifecycle := range major.SupportedEngineLifecycles {
		switch lifecycle.LifecycleSupportName {
		case rdstypes.LifecycleSupportNameOpenSourceRdsStandardSupport:
			dates.StandardSupportEnd = aws.ToTime(lifecycle.LifecycleSupportEndDate)
		case rdstypes.LifecycleSupportNameOpenSourceRdsExtendedSupport:
			end := aws.ToTime(lifecycle.LifecycleSupportEndDate)
			if !end.IsZero() {
				dates.ExtendedSupportEnd = &end
			}
		}
	}
	return dates
}

func rdsInstanceRecord(
	instance rdstypes.DBInstance,
	accountID, region string,
	now time.Time,
	majorByVersion map[engineVersionKey]string,
	lifecycleByMajor map[engineMajorKey]engineLifecycleDates,
) (Record, bool) {
	engine := strings.TrimSpace(aws.ToString(instance.Engine))
	version := strings.TrimSpace(aws.ToString(instance.EngineVersion))
	major := majorByVersion[engineVersionKey{Engine: engine, Version: version}]
	dates := lifecycleByMajor[engineMajorKey{Engine: engine, Major: major}]
	if !inExtendedSupport(now, dates.StandardSupportEnd) {
		return Record{}, false
	}
	return Record{
		AccountID:              accountID,
		Region:                 region,
		Service:                ServiceRDS,
		ResourceType:           ResourceTypeRDSInstance,
		ResourceID:             aws.ToString(instance.DBInstanceIdentifier),
		Engine:                 engine,
		EngineVersion:          version,
		MajorEngineVersion:     major,
		StandardSupportEndDate: dates.StandardSupportEnd.UTC(),
		ExtendedSupportEndDate: utcTimePtr(dates.ExtendedSupportEnd),
		EngineLifecycleSupport: aws.ToString(instance.EngineLifecycleSupport),
		Tags:                   tagMap(instance.TagList),
	}, true
}

func rdsClusterRecord(
	cluster rdstypes.DBCluster,
	accountID, region string,
	now time.Time,
	majorByVersion map[engineVersionKey]string,
	lifecycleByMajor map[engineMajorKey]engineLifecycleDates,
) (Record, bool) {
	engine := strings.TrimSpace(aws.ToString(cluster.Engine))
	version := strings.TrimSpace(aws.ToString(cluster.EngineVersion))
	major := majorByVersion[engineVersionKey{Engine: engine, Version: version}]
	dates := lifecycleByMajor[engineMajorKey{Engine: engine, Major: major}]
	if !inExtendedSupport(now, dates.StandardSupportEnd) {
		return Record{}, false
	}
	return Record{
		AccountID:              accountID,
		Region:                 region,
		Service:                ServiceRDS,
		ResourceType:           ResourceTypeRDSCluster,
		ResourceID:             aws.ToString(cluster.DBClusterIdentifier),
		Engine:                 engine,
		EngineVersion:          version,
		MajorEngineVersion:     major,
		StandardSupportEndDate: dates.StandardSupportEnd.UTC(),
		ExtendedSupportEndDate: utcTimePtr(dates.ExtendedSupportEnd),
		EngineLifecycleSupport: aws.ToString(cluster.EngineLifecycleSupport),
		Tags:                   tagMap(cluster.TagList),
	}, true
}

// inExtendedSupport reports whether a resource running the given engine major
// version is currently past its standard support end date.
func inExtendedSupport(now, standardSupportEnd time.Time) bool {
	return !standardSupportEnd.IsZero() && now.After(standardSupportEnd)
}

func listAllDBInstances(ctx context.Context, client RDSAPI, region string) ([]rdstypes.DBInstance, error) {
	var instances []rdstypes.DBInstance
	var marker *string
	for {
		out, err := client.DescribeDBInstances(ctx, &rds.DescribeDBInstancesInput{
			Marker: marker,
		})
		if err != nil {
			if isRDSRegionUnsupported(err) {
				return nil, nil
			}
			return instances, fmt.Errorf("describe db instances in %s: %w", region, err)
		}
		instances = append(instances, out.DBInstances...)
		if out.Marker == nil || aws.ToString(out.Marker) == "" {
			break
		}
		marker = out.Marker
	}
	return instances, nil
}

func listAllDBClusters(ctx context.Context, client RDSAPI, region string) ([]rdstypes.DBCluster, error) {
	var clusters []rdstypes.DBCluster
	var marker *string
	for {
		out, err := client.DescribeDBClusters(ctx, &rds.DescribeDBClustersInput{
			Marker: marker,
		})
		if err != nil {
			if isRDSRegionUnsupported(err) {
				return nil, nil
			}
			return clusters, fmt.Errorf("describe db clusters in %s: %w", region, err)
		}
		clusters = append(clusters, out.DBClusters...)
		if out.Marker == nil || aws.ToString(out.Marker) == "" {
			break
		}
		marker = out.Marker
	}
	return clusters, nil
}

func isRDSRegionUnsupported(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "not available in this region")
}

func tagMap(tags []rdstypes.Tag) map[string]string {
	if len(tags) == 0 {
		return nil
	}
	out := make(map[string]string, len(tags))
	for _, tag := range tags {
		key := strings.TrimSpace(aws.ToString(tag.Key))
		if key == "" {
			continue
		}
		out[key] = aws.ToString(tag.Value)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func utcTimePtr(t *time.Time) *time.Time {
	if t == nil || t.IsZero() {
		return nil
	}
	utc := t.UTC()
	return &utc
}
