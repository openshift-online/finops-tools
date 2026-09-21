package accountreview

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	coreaccount "github.com/openshift-online/finops-tools/core/account"
	"github.com/openshift-online/finops-tools/core/cost"
)

// Inventory table and CSV section keys. Pretty-print, JSON, CSV, and email
// all derive resource columns from AccountDetails using these keys.
const (
	SectionAccount           = "account"
	SectionTag               = "tag"
	SectionOwnerError        = "owner_error"
	SectionMonth             = "month"
	SectionTopService        = "top_service"
	SectionInventoryError    = "inventory_error"
	SectionEC2               = "ec2"
	SectionRDS               = "rds"
	SectionRDSCluster        = "rds_cluster"
	SectionRoute53           = "route53"
	SectionCount             = "count"
	CountUnattachedEBS       = "unattached_ebs"
	CountUnassociatedEIP     = "unassociated_eips"
	CountLoadBalancers       = "load_balancers"
	CountNATGateways         = "nat_gateways"
	CountS3Buckets           = "s3_buckets"
	CountLambda              = "lambda_functions"
	CountVPCs                = "vpcs"
	// NoneFoundInventoryPrefix is the pretty-print / email lead-in for NoneFoundLine.
	NoneFoundInventoryPrefix = "None found (scanned): "
)

// AccountDetails is the published account-review payload: the same fields
// shown in owner notification emails and in pretty/json/csv CLI output.
// It is a projection of AccountReport and omits raw inventory extras such as
// EC2 launch time, RDS Multi-AZ, Route53 zone IDs, and per-resource EBS/EIP lists.
type AccountDetails struct {
	AccountID      string              `json:"account_id"`
	AccountName    string              `json:"account_name"`
	DisplayAlias   string              `json:"display_alias,omitempty"`
	OUPath         string              `json:"ou_path,omitempty"`
	OwnerEmail     string              `json:"owner_email,omitempty"`
	OwnerError     string              `json:"owner_error,omitempty"`
	Tags           []coreaccount.Tag   `json:"tags,omitempty"`
	MonthlyCosts   MonthlyCostsDetails `json:"monthly_costs"`
	EC2Instances   []EC2Detail         `json:"ec2_instances,omitempty"`
	RDSInstances   []RDSDetail         `json:"rds_instances,omitempty"`
	RDSClusters    []RDSClusterDetail  `json:"rds_clusters,omitempty"`
	HostedZones    []HostedZoneDetail  `json:"hosted_zones,omitempty"`
	ResourceCounts ResourceCounts      `json:"resource_counts"`
	InventoryError string              `json:"inventory_error,omitempty"`
	GeneratedAt    time.Time           `json:"generated_at"`
}

// MonthlyCostsDetails is the cost section of AccountDetails (raw floats).
type MonthlyCostsDetails struct {
	Currency         string                  `json:"currency,omitempty"`
	Months           []cost.MonthlyCostPoint `json:"months,omitempty"`
	Total            float64                 `json:"total"`
	TopServices      []ServiceCost           `json:"top_services,omitempty"`
	TopServicesError string                  `json:"top_services_error,omitempty"`
	Error            string                  `json:"error,omitempty"`
}

// ServiceCost is one top-service row.
type ServiceCost struct {
	Service string  `json:"service"`
	Amount  float64 `json:"amount"`
}

// EC2Detail is the EC2 row shown in review output (id, name, type, state, region).
type EC2Detail struct {
	InstanceID string `json:"instance_id"`
	Name       string `json:"name,omitempty"`
	Type       string `json:"type"`
	State      string `json:"state"`
	Region     string `json:"region"`
}

// RDSDetail is the RDS instance row shown in review output.
type RDSDetail struct {
	InstanceID string `json:"instance_id"`
	Engine     string `json:"engine"`
	Class      string `json:"class"`
	Status     string `json:"status"`
	Region     string `json:"region"`
}

// RDSClusterDetail is the RDS/Aurora cluster row shown in review output.
type RDSClusterDetail struct {
	ClusterID string `json:"cluster_id"`
	Engine    string `json:"engine"`
	Status    string `json:"status"`
	Region    string `json:"region"`
}

// HostedZoneDetail is the Route53 row shown in review output (public/private, not zone id).
type HostedZoneDetail struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	RecordCount int64  `json:"record_count"`
}

// ResourceCounts holds inventory types that review output shows as counts only.
type ResourceCounts struct {
	UnattachedEBS    int `json:"unattached_ebs"`
	UnassociatedEIPs int `json:"unassociated_eips"`
	LoadBalancers    int `json:"load_balancers"`
	NATGateways      int `json:"nat_gateways"`
	S3Buckets        int `json:"s3_buckets"`
	LambdaFunctions  int `json:"lambda_functions"`
	VPCs             int `json:"vpcs"`
}

// ResourceTable is one inventory block (title, headers, string rows) shared by
// pretty-print, CSV, and email so column lists cannot drift.
type ResourceTable struct {
	Key     string
	Title   string
	Headers []string
	Rows    [][]string
	Count   int
}

// ResourceCount is one count-only inventory line with a stable key and title.
type ResourceCount struct {
	Key   string
	Title string
	Count int
}

// DetailsFrom projects an AccountReport into the published review details.
func DetailsFrom(r AccountReport) AccountDetails {
	inv := r.Inventory
	d := AccountDetails{
		AccountID:    r.AccountID,
		AccountName:  r.AccountName,
		DisplayAlias: r.DisplayAlias,
		OUPath:       r.OUPath,
		OwnerEmail:   r.OwnerEmail,
		OwnerError:   r.OwnerError,
		Tags:         r.Tags,
		MonthlyCosts: monthlyDetailsFrom(r.MonthlyCosts),
		ResourceCounts: ResourceCounts{
			UnattachedEBS:    len(inv.UnattachedEBS),
			UnassociatedEIPs: len(inv.ElasticIPs),
			LoadBalancers:    len(inv.LoadBalancers),
			NATGateways:      len(inv.NATGateways),
			S3Buckets:        len(inv.S3Buckets),
			LambdaFunctions:  len(inv.LambdaFunctions),
			VPCs:             len(inv.VPCs),
		},
		InventoryError: r.InventoryError,
		GeneratedAt:    r.GeneratedAt,
	}
	if n := len(inv.EC2Instances); n > 0 {
		d.EC2Instances = make([]EC2Detail, n)
		for i, inst := range inv.EC2Instances {
			d.EC2Instances[i] = EC2Detail{
				InstanceID: inst.InstanceID,
				Name:       inst.Name,
				Type:       inst.Type,
				State:      inst.State,
				Region:     inst.Region,
			}
		}
	}
	if n := len(inv.RDSInstances); n > 0 {
		d.RDSInstances = make([]RDSDetail, n)
		for i, db := range inv.RDSInstances {
			d.RDSInstances[i] = RDSDetail{
				InstanceID: db.InstanceID,
				Engine:     db.Engine,
				Class:      db.Class,
				Status:     db.Status,
				Region:     db.Region,
			}
		}
	}
	if n := len(inv.RDSClusters); n > 0 {
		d.RDSClusters = make([]RDSClusterDetail, n)
		for i, c := range inv.RDSClusters {
			d.RDSClusters[i] = RDSClusterDetail{
				ClusterID: c.ClusterID,
				Engine:    c.Engine,
				Status:    c.Status,
				Region:    c.Region,
			}
		}
	}
	if n := len(inv.HostedZones); n > 0 {
		d.HostedZones = make([]HostedZoneDetail, n)
		for i, z := range inv.HostedZones {
			d.HostedZones[i] = HostedZoneDetail{
				Name:        z.Name,
				Type:        hostedZoneType(z.PrivateZone),
				RecordCount: z.RecordCount,
			}
		}
	}
	return d
}

// DetailsFromReports projects each report. The returned slice is never nil.
func DetailsFromReports(reports []AccountReport) []AccountDetails {
	out := make([]AccountDetails, len(reports))
	for i, r := range reports {
		out[i] = DetailsFrom(r)
	}
	return out
}

// InventoryTables returns the detailed resource tables in display order.
func (d AccountDetails) InventoryTables() []ResourceTable {
	ec2Rows := make([][]string, len(d.EC2Instances))
	for i, inst := range d.EC2Instances {
		ec2Rows[i] = []string{inst.InstanceID, dashIfEmpty(inst.Name), inst.Type, inst.State, inst.Region}
	}
	rdsRows := make([][]string, len(d.RDSInstances))
	for i, db := range d.RDSInstances {
		rdsRows[i] = []string{db.InstanceID, db.Engine, db.Class, db.Status, db.Region}
	}
	clusterRows := make([][]string, len(d.RDSClusters))
	for i, c := range d.RDSClusters {
		clusterRows[i] = []string{c.ClusterID, c.Engine, c.Status, c.Region}
	}
	zoneRows := make([][]string, len(d.HostedZones))
	for i, z := range d.HostedZones {
		zoneRows[i] = []string{z.Name, z.Type, fmt.Sprintf("%d", z.RecordCount)}
	}
	return []ResourceTable{
		{Key: SectionEC2, Title: "EC2 instances", Headers: []string{"ID", "Name", "Type", "State", "Region"}, Rows: ec2Rows, Count: len(d.EC2Instances)},
		{Key: SectionRDS, Title: "RDS instances", Headers: []string{"ID", "Engine", "Class", "Status", "Region"}, Rows: rdsRows, Count: len(d.RDSInstances)},
		{Key: SectionRDSCluster, Title: "RDS clusters", Headers: []string{"ID", "Engine", "Status", "Region"}, Rows: clusterRows, Count: len(d.RDSClusters)},
		{Key: SectionRoute53, Title: "Route53 hosted zones", Headers: []string{"Name", "Type", "Records"}, Rows: zoneRows, Count: len(d.HostedZones)},
	}
}

// InventoryCounts returns count-only inventory lines in display order.
func (d AccountDetails) InventoryCounts() []ResourceCount {
	c := d.ResourceCounts
	return []ResourceCount{
		{Key: CountUnattachedEBS, Title: "Unattached EBS volumes", Count: c.UnattachedEBS},
		{Key: CountUnassociatedEIP, Title: "Unassociated Elastic IPs", Count: c.UnassociatedEIPs},
		{Key: CountLoadBalancers, Title: "Load balancers", Count: c.LoadBalancers},
		{Key: CountNATGateways, Title: "NAT gateways", Count: c.NATGateways},
		{Key: CountS3Buckets, Title: "S3 buckets", Count: c.S3Buckets},
		{Key: CountLambda, Title: "Lambda functions", Count: c.LambdaFunctions},
		{Key: CountVPCs, Title: "VPCs", Count: c.VPCs},
	}
}

// NonemptyInventoryTables returns InventoryTables entries with Count > 0.
func (d AccountDetails) NonemptyInventoryTables() []ResourceTable {
	var out []ResourceTable
	for _, table := range d.InventoryTables() {
		if table.Count > 0 {
			out = append(out, table)
		}
	}
	return out
}

// NonzeroInventoryCounts returns InventoryCounts entries with Count > 0.
func (d AccountDetails) NonzeroInventoryCounts() []ResourceCount {
	var out []ResourceCount
	for _, c := range d.InventoryCounts() {
		if c.Count > 0 {
			out = append(out, c)
		}
	}
	return out
}

// EmptyInventoryTitles lists resource types that were scanned and found empty,
// in the same order as InventoryTables then InventoryCounts.
// When InventoryError is set the scan was incomplete, so zeros are not
// confirmed empty and this returns nil.
func (d AccountDetails) EmptyInventoryTitles() []string {
	if strings.TrimSpace(d.InventoryError) != "" {
		return nil
	}
	var titles []string
	for _, table := range d.InventoryTables() {
		if table.Count == 0 {
			titles = append(titles, table.Title)
		}
	}
	for _, c := range d.InventoryCounts() {
		if c.Count == 0 {
			titles = append(titles, c.Title)
		}
	}
	return titles
}

// NoneFoundLine returns a single display line for scanned-empty resource types.
// It is empty when the scan was incomplete or every type has a non-zero count.
func (d AccountDetails) NoneFoundLine() string {
	titles := d.EmptyInventoryTitles()
	if len(titles) == 0 {
		return ""
	}
	return NoneFoundInventoryPrefix + strings.Join(titles, ", ")
}

// MarshalJSON omits zero resource_counts fields when InventoryError is set so
// JSON consumers cannot treat an incomplete scan as confirmed-empty inventory.
func (d AccountDetails) MarshalJSON() ([]byte, error) {
	type detailsJSON AccountDetails
	if strings.TrimSpace(d.InventoryError) == "" {
		return json.Marshal(detailsJSON(d))
	}
	return json.Marshal(struct {
		*detailsJSON
		ResourceCounts resourceCountsOmitZero `json:"resource_counts"`
	}{
		detailsJSON:    (*detailsJSON)(&d),
		ResourceCounts: resourceCountsOmitZero(d.ResourceCounts),
	})
}

type resourceCountsOmitZero struct {
	UnattachedEBS    int `json:"unattached_ebs,omitempty"`
	UnassociatedEIPs int `json:"unassociated_eips,omitempty"`
	LoadBalancers    int `json:"load_balancers,omitempty"`
	NATGateways      int `json:"nat_gateways,omitempty"`
	S3Buckets        int `json:"s3_buckets,omitempty"`
	LambdaFunctions  int `json:"lambda_functions,omitempty"`
	VPCs             int `json:"vpcs,omitempty"`
}

func monthlyDetailsFrom(m cost.AccountMonthlyCosts) MonthlyCostsDetails {
	out := MonthlyCostsDetails{
		Currency:         m.Currency,
		Months:           m.Months,
		Total:            m.Total,
		TopServicesError: m.TopServicesError,
		Error:            m.Error,
	}
	if n := len(m.TopServices); n > 0 {
		out.TopServices = make([]ServiceCost, n)
		for i, svc := range m.TopServices {
			out.TopServices[i] = ServiceCost{Service: svc.Service, Amount: svc.Amount}
		}
	}
	return out
}

func hostedZoneType(private bool) string {
	if private {
		return "private"
	}
	return "public"
}

func dashIfEmpty(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
