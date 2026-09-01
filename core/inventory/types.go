// Package inventory discovers AWS resources across regions for account review emails.
//
// Scans are best-effort: a failed service, region, or account is recorded as a
// warning so the rest of the inventory (and other accounts) still appear.
package inventory

import (
	"context"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
)

// AccountTarget holds credentials scoped to one AWS account for inventory scans.
type AccountTarget struct {
	AccountID    string
	DisplayName  string
	DisplayAlias string
	AWSConfig    aws.Config
	// ConfigLoader, when set, is called at scan time (linked-account assume-role).
	// AWSConfig is used when ConfigLoader is nil (payer / already-loaded session).
	ConfigLoader func(context.Context) (aws.Config, error) `json:"-"`
}

// Query describes an inventory scan request.
type Query struct {
	Targets      []AccountTarget
	Regions      []string
	Workers      int
	Now          time.Time
	OnProgress   func(message string)
	regionLister regionLister
	scanner      accountScanner
}

// Result holds inventory for all scanned accounts.
type Result struct {
	Accounts []AccountInventory `json:"accounts"`
}

// AccountInventory is the full resource inventory for one account.
type AccountInventory struct {
	AccountID       string           `json:"account_id"`
	EC2Instances    []EC2Instance    `json:"ec2_instances,omitempty"`
	RDSInstances    []RDSInstance    `json:"rds_instances,omitempty"`
	RDSClusters     []RDSCluster     `json:"rds_clusters,omitempty"`
	HostedZones     []HostedZone     `json:"hosted_zones,omitempty"`
	UnattachedEBS   []EBSVolume      `json:"unattached_ebs,omitempty"`
	ElasticIPs      []ElasticIP      `json:"elastic_ips,omitempty"`
	LoadBalancers   []LoadBalancer   `json:"load_balancers,omitempty"`
	NATGateways     []NATGateway     `json:"nat_gateways,omitempty"`
	S3Buckets       []S3Bucket       `json:"s3_buckets,omitempty"`
	LambdaFunctions []LambdaFunction `json:"lambda_functions,omitempty"`
	VPCs            []VPC            `json:"vpcs,omitempty"`
	// SkippedRegions records per-region service API failures (EC2, RDS, ELB, Lambda).
	SkippedRegions []RegionWarning `json:"skipped_regions,omitempty"`
	// Warnings records account-level failures (credential load, list regions, Route53, S3).
	Warnings []string `json:"warnings,omitempty"`
}

// EC2Instance is a running or stopped EC2 instance.
type EC2Instance struct {
	InstanceID string    `json:"instance_id"`
	Name       string    `json:"name,omitempty"`
	Type       string    `json:"type"`
	State      string    `json:"state"`
	Region     string    `json:"region"`
	LaunchTime time.Time `json:"launch_time,omitempty"`
}

// RDSInstance is a standalone RDS DB instance (not a member of an Aurora/RDS cluster).
type RDSInstance struct {
	InstanceID string `json:"instance_id"`
	Engine     string `json:"engine"`
	Class      string `json:"class"`
	Status     string `json:"status"`
	MultiAZ    bool   `json:"multi_az"`
	Region     string `json:"region"`
}

// RDSCluster is an RDS/Aurora DB cluster.
type RDSCluster struct {
	ClusterID string `json:"cluster_id"`
	Engine    string `json:"engine"`
	Status    string `json:"status"`
	Region    string `json:"region"`
}

// HostedZone is a Route53 hosted zone.
type HostedZone struct {
	ZoneID      string `json:"zone_id"`
	Name        string `json:"name"`
	PrivateZone bool   `json:"private_zone"`
	RecordCount int64  `json:"record_count"`
}

// EBSVolume is an unattached EBS volume.
type EBSVolume struct {
	VolumeID string `json:"volume_id"`
	SizeGiB  int32  `json:"size_gib"`
	Region   string `json:"region"`
	State    string `json:"state"`
}

// ElasticIP is an unassociated Elastic IP address.
type ElasticIP struct {
	PublicIP string `json:"public_ip"`
	Region   string `json:"region"`
}

// LoadBalancer is a classic or v2 load balancer.
type LoadBalancer struct {
	Name   string `json:"name"`
	Type   string `json:"type"`
	State  string `json:"state"`
	Region string `json:"region"`
}

// NATGateway is a NAT gateway.
type NATGateway struct {
	GatewayID string `json:"gateway_id"`
	State     string `json:"state"`
	Region    string `json:"region"`
}

// S3Bucket is an S3 bucket.
type S3Bucket struct {
	Name   string `json:"name"`
	Region string `json:"region"`
}

// LambdaFunction is a Lambda function.
type LambdaFunction struct {
	Name   string `json:"name"`
	Region string `json:"region"`
}

// VPC is a virtual private cloud.
type VPC struct {
	VPCID     string `json:"vpc_id"`
	Region    string `json:"region"`
	IsDefault bool   `json:"is_default"`
}

// RegionWarning records a regional scan failure.
type RegionWarning struct {
	AccountID string `json:"account_id"`
	Region    string `json:"region"`
	Message   string `json:"message"`
}
