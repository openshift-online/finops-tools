// Package extendedsupport discovers AWS resources running engine or platform
// versions past their standard support end date (AWS Extended Support).
package extendedsupport

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
)

// Service identifies an AWS service scanned for extended-support resources.
type Service string

const (
	// ServiceRDS covers RDS DB instances and Aurora/RDS multi-AZ DB clusters.
	ServiceRDS Service = "rds"
)

// ParseServices parses a comma-separated --services value.
// An empty value selects every supported service.
func ParseServices(s string) ([]Service, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return []Service{ServiceRDS}, nil
	}
	var services []Service
	seen := make(map[Service]struct{})
	for _, part := range strings.Split(s, ",") {
		part = strings.ToLower(strings.TrimSpace(part))
		if part == "" {
			continue
		}
		var service Service
		switch part {
		case "rds":
			service = ServiceRDS
		default:
			return nil, fmt.Errorf("unknown service %q (supported: rds)", part)
		}
		if _, ok := seen[service]; ok {
			continue
		}
		seen[service] = struct{}{}
		services = append(services, service)
	}
	if len(services) == 0 {
		return nil, fmt.Errorf("at least one service is required")
	}
	return services, nil
}

// ResourceType identifies the kind of extended-support resource.
type ResourceType string

const (
	ResourceTypeRDSInstance ResourceType = "rds-instance"
	ResourceTypeRDSCluster  ResourceType = "rds-cluster"
)

// AccountTarget holds credentials scoped to one AWS account for a scan.
type AccountTarget struct {
	AccountID    string
	DisplayName  string
	DisplayAlias string
	AWSConfig    aws.Config
	// ConfigLoader, when set, is called at scan time (linked-account assume-role).
	ConfigLoader func(context.Context) (aws.Config, error) `json:"-"`
}

// AccountProgress reports completion of per-account scans.
type AccountProgress interface {
	Advance()
}

// Query describes an extended-support discovery request.
type Query struct {
	Targets         []AccountTarget
	Services        []Service
	Regions         []string
	AccountProgress AccountProgress
	Now             time.Time
	// Workers bounds concurrent account scans (0 = default).
	Workers      int
	regionLister regionLister
	rdsLister    rdsLister
}

// Record is one resource currently past its standard support end date.
type Record struct {
	AccountID              string            `json:"account_id"`
	Region                 string            `json:"region"`
	Service                Service           `json:"service"`
	ResourceType           ResourceType      `json:"resource_type"`
	ResourceID             string            `json:"resource_id"`
	Engine                 string            `json:"engine"`
	EngineVersion          string            `json:"engine_version"`
	MajorEngineVersion     string            `json:"major_engine_version"`
	StandardSupportEndDate time.Time         `json:"standard_support_end_date"`
	ExtendedSupportEndDate *time.Time        `json:"extended_support_end_date,omitempty"`
	EngineLifecycleSupport string            `json:"engine_lifecycle_support,omitempty"`
	Tags                   map[string]string `json:"tags,omitempty"`
}

// ServiceSummary aggregates records by service.
type ServiceSummary struct {
	Service Service `json:"service"`
	Count   int     `json:"count"`
}

// AccountSummary aggregates records by account.
type AccountSummary struct {
	AccountID string `json:"account_id"`
	Count     int    `json:"count"`
}

// RegionWarning records a region that could not be scanned.
type RegionWarning struct {
	AccountID string `json:"account_id"`
	Region    string `json:"region"`
	Message   string `json:"message"`
}

// AccountWarning records an account that could not be prepared for scanning.
type AccountWarning struct {
	AccountID    string `json:"account_id"`
	DisplayAlias string `json:"display_alias,omitempty"`
	Message      string `json:"message"`
}

// ResourceWarning identifies a resource whose support status could not be determined.
type ResourceWarning struct {
	AccountID     string       `json:"account_id"`
	Region        string       `json:"region"`
	ResourceType  ResourceType `json:"resource_type"`
	ResourceID    string       `json:"resource_id"`
	Engine        string       `json:"engine"`
	EngineVersion string       `json:"engine_version"`
	Message       string       `json:"message"`
}

// regionScanResult retains discoveries alongside failures in other parts of a region.
type regionScanResult struct {
	Records               []Record
	Warnings              []RegionWarning
	UnclassifiedResources []ResourceWarning
}

// Summary holds aggregate totals for an extended-support scan.
type Summary struct {
	TotalCount            int               `json:"total_count"`
	ByService             []ServiceSummary  `json:"by_service,omitempty"`
	ByAccount             []AccountSummary  `json:"by_account,omitempty"`
	SkippedRegions        []RegionWarning   `json:"skipped_regions,omitempty"`
	SkippedAccounts       []AccountWarning  `json:"skipped_accounts,omitempty"`
	UnclassifiedResources []ResourceWarning `json:"unclassified_resources,omitempty"`
}

// Result is the output of Fetch.
type Result struct {
	Records []Record `json:"records"`
	Summary Summary  `json:"summary"`
}

// ParseRegions parses a comma-separated --regions flag.
func ParseRegions(s string) ([]string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}
	var out []string
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		out = append(out, part)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("at least one region is required")
	}
	return out, nil
}
