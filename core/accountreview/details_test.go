package accountreview

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	coreaccount "github.com/openshift-online/finops-tools/core/account"
	"github.com/openshift-online/finops-tools/core/cost"
	"github.com/openshift-online/finops-tools/core/inventory"
)

func TestDetailsFromEmailParity(t *testing.T) {
	t.Parallel()

	generated := time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC)
	report := AccountReport{
		AccountID:    "111111111111",
		AccountName:  "test-account",
		DisplayAlias: "my-linked",
		OUPath:       "Root / Sandbox",
		OwnerEmail:   "jdoe@redhat.com",
		Tags:         []coreaccount.Tag{{Key: "owner", Value: "jdoe"}},
		GeneratedAt:  generated,
		MonthlyCosts: cost.AccountMonthlyCosts{
			AccountID: "111111111111",
			Currency:  "USD",
			Months:    []cost.MonthlyCostPoint{{Month: "2026-01", Amount: 1234.5}},
			Total:     1234.5,
			TopServices: []cost.CostBreakdownItem{
				{Service: "Amazon Elastic Compute Cloud - Compute", Amount: 100, Account: "ignored"},
			},
		},
		Inventory: inventory.AccountInventory{
			EC2Instances: []inventory.EC2Instance{{
				InstanceID: "i-abc",
				Name:       "web",
				Type:       "t3.micro",
				State:      "running",
				Region:     "us-east-1",
				LaunchTime: generated,
			}},
			RDSInstances: []inventory.RDSInstance{{
				InstanceID: "db-1",
				Engine:     "postgres",
				Class:      "db.t3.micro",
				Status:     "available",
				MultiAZ:    true,
				Region:     "us-east-1",
			}},
			RDSClusters: []inventory.RDSCluster{{
				ClusterID: "cluster-1",
				Engine:    "aurora-postgresql",
				Status:    "available",
				Region:    "us-east-1",
			}},
			HostedZones: []inventory.HostedZone{{
				ZoneID:      "Z123",
				Name:        "example.com.",
				PrivateZone: true,
				RecordCount: 4,
			}},
			UnattachedEBS:   []inventory.EBSVolume{{VolumeID: "vol-1", SizeGiB: 10, Region: "us-east-1"}},
			ElasticIPs:      []inventory.ElasticIP{{PublicIP: "1.2.3.4", Region: "us-east-1"}},
			LoadBalancers:   []inventory.LoadBalancer{{Name: "alb-1"}},
			NATGateways:     []inventory.NATGateway{{GatewayID: "nat-1"}},
			S3Buckets:       []inventory.S3Bucket{{Name: "bucket"}},
			LambdaFunctions: []inventory.LambdaFunction{{Name: "fn"}},
			VPCs:            []inventory.VPC{{VPCID: "vpc-1"}, {VPCID: "vpc-2"}},
		},
		InventoryError: "us-west-2: access denied",
	}

	d := DetailsFrom(report)
	if d.AccountID != "111111111111" || d.AccountName != "test-account" {
		t.Fatalf("identity = %+v", d)
	}
	if d.DisplayAlias != "my-linked" || d.OUPath != "Root / Sandbox" {
		t.Fatalf("alias/ou = %+v", d)
	}
	if d.OwnerEmail != "jdoe@redhat.com" || len(d.Tags) != 1 {
		t.Fatalf("owner/tags = %+v", d)
	}
	if d.MonthlyCosts.Total != 1234.5 || d.MonthlyCosts.Currency != "USD" {
		t.Fatalf("monthly = %+v", d.MonthlyCosts)
	}
	if len(d.MonthlyCosts.TopServices) != 1 || d.MonthlyCosts.TopServices[0].Service == "" {
		t.Fatalf("top services = %+v", d.MonthlyCosts.TopServices)
	}
	if d.MonthlyCosts.TopServices[0].Amount != 100 {
		t.Fatalf("top service amount = %v", d.MonthlyCosts.TopServices[0].Amount)
	}

	if len(d.EC2Instances) != 1 {
		t.Fatalf("ec2 = %+v", d.EC2Instances)
	}
	ec2 := d.EC2Instances[0]
	if ec2.InstanceID != "i-abc" || ec2.Name != "web" || ec2.Type != "t3.micro" || ec2.State != "running" || ec2.Region != "us-east-1" {
		t.Fatalf("ec2 row = %+v", ec2)
	}

	if len(d.RDSInstances) != 1 || d.RDSInstances[0].InstanceID != "db-1" || d.RDSInstances[0].Class != "db.t3.micro" {
		t.Fatalf("rds = %+v", d.RDSInstances)
	}
	if len(d.RDSClusters) != 1 || d.RDSClusters[0].ClusterID != "cluster-1" {
		t.Fatalf("clusters = %+v", d.RDSClusters)
	}
	if len(d.HostedZones) != 1 {
		t.Fatalf("zones = %+v", d.HostedZones)
	}
	z := d.HostedZones[0]
	if z.Name != "example.com." || z.Type != "private" || z.RecordCount != 4 {
		t.Fatalf("zone = %+v", z)
	}

	c := d.ResourceCounts
	if c.UnattachedEBS != 1 || c.UnassociatedEIPs != 1 || c.LoadBalancers != 1 || c.NATGateways != 1 || c.S3Buckets != 1 || c.LambdaFunctions != 1 || c.VPCs != 2 {
		t.Fatalf("counts = %+v", c)
	}
	if d.InventoryError != "us-west-2: access denied" {
		t.Fatalf("inventory error = %q", d.InventoryError)
	}
	if !d.GeneratedAt.Equal(generated) {
		t.Fatalf("generated_at = %v", d.GeneratedAt)
	}

	tables := d.InventoryTables()
	if len(tables) != 4 {
		t.Fatalf("tables = %d", len(tables))
	}
	wantHeaders := map[string][]string{
		SectionEC2:        {"ID", "Name", "Type", "State", "Region"},
		SectionRDS:        {"ID", "Engine", "Class", "Status", "Region"},
		SectionRDSCluster: {"ID", "Engine", "Status", "Region"},
		SectionRoute53:    {"Name", "Type", "Records"},
	}
	for _, table := range tables {
		want, ok := wantHeaders[table.Key]
		if !ok {
			t.Fatalf("unexpected table key %q", table.Key)
		}
		if !stringSlicesEqual(table.Headers, want) {
			t.Fatalf("%s headers = %v, want %v", table.Key, table.Headers, want)
		}
	}
	if got := tables[0].Rows[0]; got[1] != "web" {
		t.Fatalf("ec2 name cell = %v", got)
	}
	if got := tables[3].Rows[0]; got[1] != "private" || got[2] != "4" {
		t.Fatalf("route53 row = %v", got)
	}

	counts := d.InventoryCounts()
	if len(counts) != 7 {
		t.Fatalf("count lines = %d", len(counts))
	}
	if counts[0].Key != CountUnattachedEBS || counts[0].Count != 1 {
		t.Fatalf("first count = %+v", counts[0])
	}
	if counts[6].Key != CountVPCs || counts[6].Count != 2 {
		t.Fatalf("vpc count = %+v", counts[6])
	}
}

func TestDetailsFromEmptyEC2NameUsesDashInTable(t *testing.T) {
	t.Parallel()
	d := DetailsFrom(AccountReport{
		Inventory: inventory.AccountInventory{
			EC2Instances: []inventory.EC2Instance{{InstanceID: "i-1", Type: "t3.micro", State: "stopped", Region: "us-west-2"}},
		},
	})
	if d.EC2Instances[0].Name != "" {
		t.Fatalf("json name should stay empty, got %q", d.EC2Instances[0].Name)
	}
	row := d.InventoryTables()[0].Rows[0]
	if row[1] != "-" {
		t.Fatalf("table name cell = %q", row[1])
	}
}

func TestDetailsFromReportsNeverNil(t *testing.T) {
	t.Parallel()
	got := DetailsFromReports(nil)
	if got == nil {
		t.Fatal("DetailsFromReports(nil) returned nil")
	}
	if len(got) != 0 {
		t.Fatalf("len = %d", len(got))
	}
}

func TestNonemptyInventoryFiltersZeros(t *testing.T) {
	t.Parallel()
	d := AccountDetails{
		EC2Instances:   []EC2Detail{{InstanceID: "i-1", Type: "t3.micro", State: "running", Region: "us-east-1"}},
		ResourceCounts: ResourceCounts{VPCs: 3},
	}
	tables := d.NonemptyInventoryTables()
	if len(tables) != 1 || tables[0].Key != SectionEC2 || tables[0].Count != 1 {
		t.Fatalf("tables = %+v", tables)
	}
	counts := d.NonzeroInventoryCounts()
	if len(counts) != 1 || counts[0].Key != CountVPCs || counts[0].Count != 3 {
		t.Fatalf("counts = %+v", counts)
	}
	empty := AccountDetails{}
	if got := empty.NonemptyInventoryTables(); len(got) != 0 {
		t.Fatalf("empty tables = %+v", got)
	}
	if got := empty.NonzeroInventoryCounts(); len(got) != 0 {
		t.Fatalf("empty counts = %+v", got)
	}
	emptyTitles := empty.EmptyInventoryTitles()
	wantEmpty := len(empty.InventoryTables()) + len(empty.InventoryCounts())
	if len(emptyTitles) != wantEmpty {
		t.Fatalf("empty titles = %v (len %d, want %d)", emptyTitles, len(emptyTitles), wantEmpty)
	}
	if stringsContain(emptyTitles, "OpenShift clusters") {
		t.Fatalf("OpenShift must not appear in AWS empty titles: %v", emptyTitles)
	}
	foundTitles := d.EmptyInventoryTitles()
	if stringsContain(foundTitles, "EC2 instances") || stringsContain(foundTitles, "VPCs") {
		t.Fatalf("nonempty types listed as empty: %v", foundTitles)
	}
	if !stringsContain(foundTitles, "RDS instances") || !stringsContain(foundTitles, "Unattached EBS volumes") {
		t.Fatalf("missing empty types: %v", foundTitles)
	}
}

func TestEmptyInventoryTitlesOmitsWhenScanIncomplete(t *testing.T) {
	t.Parallel()
	d := AccountDetails{
		InventoryError: "us-west-2: scan timed out after 45s",
		ResourceCounts: ResourceCounts{VPCs: 3},
	}
	if got := d.EmptyInventoryTitles(); got != nil {
		t.Fatalf("incomplete AWS scan must not claim types were empty: %v", got)
	}
	if got := d.NoneFoundLine(); got != "" {
		t.Fatalf("incomplete AWS scan must not emit a none-found line: %q", got)
	}
}

func TestOpenShiftNoneFoundLineIndependentOfAWSInventoryError(t *testing.T) {
	t.Parallel()
	d := AccountDetails{
		InventoryError:    "us-west-2: denied",
		OpenShiftClusters: []OpenShiftClusterDetail{},
	}
	if got := d.EmptyInventoryTitles(); got != nil {
		t.Fatalf("incomplete AWS scan must not claim AWS types empty: %v", got)
	}
	if got := d.OpenShiftNoneFoundLine(); !strings.Contains(got, "OpenShift clusters") {
		t.Fatalf("OpenShift none-found = %q", got)
	}
	d.OpenShiftClustersError = "snowflake unavailable"
	d.OpenShiftClusters = nil
	if got := d.OpenShiftNoneFoundLine(); got != "" {
		t.Fatalf("failed OCM lookup must not claim OpenShift empty: %q", got)
	}
}

func TestDetailsFromOpenShiftClusters(t *testing.T) {
	t.Parallel()
	d := DetailsFrom(AccountReport{
		OpenShiftClusters: []OpenShiftClusterDetail{{
			Environment: "Production",
			Name:        "my-cluster",
			ClusterID:   "ocm-1",
			ProductType: "ROSA Classic",
			State:       "ready",
			Region:      "us-east-1",
		}},
	})
	if len(d.OpenShiftClusters) != 1 || d.OpenShiftClusters[0].Name != "my-cluster" {
		t.Fatalf("clusters = %+v", d.OpenShiftClusters)
	}
	table := d.OpenShiftTable()
	if table.Key != SectionOpenShift || table.Count != 1 || table.Rows[0][2] != "ocm-1" {
		t.Fatalf("openshift table = %+v", table)
	}
	if d.HasOpenShiftSection() != true {
		t.Fatal("expected OpenShift section")
	}
	if got := d.OpenShiftSectionTitle(); got != "OpenShift clusters (1)" {
		t.Fatalf("title = %q", got)
	}
}

func TestDetailsFromPreservesEmptyOpenShiftClusters(t *testing.T) {
	t.Parallel()
	d := DetailsFrom(AccountReport{
		OpenShiftClusters: []OpenShiftClusterDetail{},
	})
	if d.OpenShiftClusters == nil {
		t.Fatal("successful zero-cluster lookup must stay non-nil after DetailsFrom")
	}
	if !d.HasOpenShiftSection() {
		t.Fatal("expected OpenShift section for successful empty lookup")
	}
	if got := d.OpenShiftSectionTitle(); got != "OpenShift clusters (0)" {
		t.Fatalf("title = %q", got)
	}
}

func TestOpenShiftSectionTitleUnavailableOnFailure(t *testing.T) {
	t.Parallel()
	d := AccountDetails{OpenShiftClustersError: "snowflake unavailable"}
	if got := d.OpenShiftSectionTitle(); got != "OpenShift clusters (unavailable)" {
		t.Fatalf("failed lookup title = %q", got)
	}
	d = AccountDetails{OpenShiftClusters: []OpenShiftClusterDetail{}}
	if got := d.OpenShiftSectionTitle(); got != "OpenShift clusters (0)" {
		t.Fatalf("empty success title = %q", got)
	}
}

func TestNoneFoundLineJoinsEmptyTitles(t *testing.T) {
	t.Parallel()
	d := AccountDetails{ResourceCounts: ResourceCounts{VPCs: 3}}
	got := d.NoneFoundLine()
	if !strings.HasPrefix(got, NoneFoundInventoryPrefix) {
		t.Fatalf("prefix missing: %q", got)
	}
	if !strings.Contains(got, "EC2 instances") || strings.Contains(got, "VPCs") {
		t.Fatalf("line = %q", got)
	}
}

func TestAccountDetailsJSONOmitsZeroCountsWhenIncomplete(t *testing.T) {
	t.Parallel()
	incomplete := AccountDetails{
		AccountID:      "111111111111",
		InventoryError: "us-west-2: denied",
		ResourceCounts: ResourceCounts{VPCs: 3},
	}
	raw, err := json.Marshal(incomplete)
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	if !strings.Contains(s, `"vpcs":3`) {
		t.Fatalf("nonzero count missing: %s", s)
	}
	if strings.Contains(s, `"unattached_ebs"`) || strings.Contains(s, `"load_balancers"`) {
		t.Fatalf("zero counts must be omitted on incomplete scan: %s", s)
	}

	complete := AccountDetails{
		AccountID:      "111111111111",
		ResourceCounts: ResourceCounts{VPCs: 3},
	}
	raw, err = json.Marshal(complete)
	if err != nil {
		t.Fatal(err)
	}
	s = string(raw)
	if !strings.Contains(s, `"unattached_ebs":0`) || !strings.Contains(s, `"load_balancers":0`) {
		t.Fatalf("complete scan should emit zero counts: %s", s)
	}
}

func stringsContain(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

func stringSlicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
