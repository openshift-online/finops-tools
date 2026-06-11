package hcphierarchy

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
)

// mockRows simulates Snowflake row iteration over pre-loaded test data.
type mockRows struct {
	data  [][]any
	index int
	err   error
}

func (m *mockRows) Next() bool  { m.index++; return m.index <= len(m.data) }
func (m *mockRows) Close() error { return nil }
func (m *mockRows) Err() error   { return m.err }
func (m *mockRows) Scan(dest ...any) error {
	row := m.data[m.index-1]
	for i, d := range dest {
		if i >= len(row) {
			continue
		}
		switch p := d.(type) {
		case *string:
			if row[i] == nil {
				*p = ""
			} else if s, ok := row[i].(string); ok {
				*p = s
			}
		case **bool:
			if b, ok := row[i].(bool); ok {
				v := b
				*p = &v
			}
		case **string:
			if row[i] == nil {
				*p = nil
			} else if s, ok := row[i].(string); ok {
				v := s
				*p = &v
			}
		}
	}
	return nil
}

// mockQueryer returns the configured mockRows for any query.
type mockQueryer struct {
	rows *mockRows
	err  error
}

func (m *mockQueryer) QueryContext(_ context.Context, _ string, _ ...any) (SnowflakeRows, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.rows, nil
}

// makeWorkerRow builds a full 25-column test row.
func makeWorkerRow(
	ocmID, clusterID, clusterName, accountID, state, region string,
	org, classification, classReason string,
	hierarchyComplete bool,
	hierarchySource string,
	mcName, mcFleetID, mcAccountID, mcRegion, mcStatus string,
	scName, scFleetID, scAccountID, scRegion, scStatus string,
	sector, billing, serviceLevel, subscriptionStatus string,
) []any {
	return []any{
		ocmID, clusterID, clusterName, accountID, state, region,
		org, classification, classReason,
		hierarchyComplete,
		hierarchySource,
		mcName, mcFleetID, mcAccountID, mcRegion, mcStatus,
		scName, scFleetID, scAccountID, scRegion, scStatus,
		sector, billing, serviceLevel, subscriptionStatus,
	}
}

func TestBuild_ReturnsSingleWorkerRow(t *testing.T) {
	mq := &mockQueryer{rows: &mockRows{data: [][]any{
		makeWorkerRow(
			"ocm-uuid-001", "ext-uuid-001", "my-worker", "111111111111",
			"ready", "us-east-1",
			"MyOrg", "HCP", "hcp_worker",
			true, "mart",
			"mc-name", "mc-fleet-id", "222222222222", "us-east-1", "ready",
			"sc-name", "sc-fleet-id", "333333333333", "us-east-1", "ready",
			"staging", "marketplace", "standard", "active",
		),
	}}}

	report, err := Build(context.Background(), mq, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(report.Rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(report.Rows))
	}

	row := report.Rows[0]
	if row.CustomerClusterID != "ext-uuid-001" {
		t.Errorf("CustomerClusterID = %q, want ext-uuid-001", row.CustomerClusterID)
	}
	if row.MCAccountID != "222222222222" {
		t.Errorf("MCAccountID = %q, want 222222222222", row.MCAccountID)
	}
	if row.SCAccountID != "333333333333" {
		t.Errorf("SCAccountID = %q, want 333333333333", row.SCAccountID)
	}
	if !row.HierarchyComplete {
		t.Error("expected HierarchyComplete = true")
	}
	if report.MartView != defaultMartView {
		t.Errorf("MartView = %q, want default", report.MartView)
	}
	if report.GeneratedAt.IsZero() {
		t.Error("GeneratedAt should not be zero")
	}
}

func TestBuild_MultipleRows(t *testing.T) {
	mq := &mockQueryer{rows: &mockRows{data: [][]any{
		makeWorkerRow(
			"ocm-1", "cluster-1", "worker-1", "111111111111",
			"ready", "us-east-1", "Org1", "HCP", "hcp_worker", true, "mart",
			"mc1", "mc-fleet-1", "222222222222", "us-east-1", "ready",
			"sc1", "sc-fleet-1", "333333333333", "us-east-1", "ready",
			"prod", "standard", "premium", "active",
		),
		makeWorkerRow(
			"ocm-2", "cluster-2", "worker-2", "444444444444",
			"ready", "eu-west-1", "Org2", "HCP", "hcp_worker", false, "mart",
			"mc2", "mc-fleet-2", "555555555555", "eu-west-1", "ready",
			"sc2", "sc-fleet-2", "666666666666", "eu-west-1", "ready",
			"staging", "marketplace", "standard", "active",
		),
	}}}

	report, err := Build(context.Background(), mq, "MY_DB.SCHEMA.TABLE")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(report.Rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(report.Rows))
	}
	if report.MartView != "MY_DB.SCHEMA.TABLE" {
		t.Errorf("MartView = %q, want MY_DB.SCHEMA.TABLE", report.MartView)
	}
	if report.Rows[1].CustomerRegion != "eu-west-1" {
		t.Errorf("Rows[1].CustomerRegion = %q, want eu-west-1", report.Rows[1].CustomerRegion)
	}
}

func TestBuild_EmptyResult(t *testing.T) {
	mq := &mockQueryer{rows: &mockRows{data: nil}}
	report, err := Build(context.Background(), mq, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(report.Rows) != 0 {
		t.Errorf("expected 0 rows, got %d", len(report.Rows))
	}
}

func TestBuild_QueryError(t *testing.T) {
	mq := &mockQueryer{err: fmt.Errorf("access denied to mart")}
	_, err := Build(context.Background(), mq, "")
	if err == nil {
		t.Fatal("expected error from query failure, got nil")
	}
	if !strings.Contains(err.Error(), "mart query") {
		t.Errorf("error should mention mart query, got: %v", err)
	}
}

func TestBuild_NullableStringColumns(t *testing.T) {
	row := makeWorkerRow(
		"ocm-nil", "cluster-nil", "worker-nil", "111111111111",
		"ready", "us-east-1", "OrgX", "HCP", "hcp_worker", true, "mart",
		"mc", "mc-f", "222222222222", "us-east-1", "ready",
		"sc", "sc-f", "333333333333", "us-east-1", "ready",
		"prod", "standard", "premium", "active",
	)
	row[11] = nil // mc_name
	row[22] = nil // billing_model
	row[23] = nil // service_level
	row[24] = nil // subscription_status

	mq := &mockQueryer{rows: &mockRows{data: [][]any{row}}}
	report, err := Build(context.Background(), mq, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := report.Rows[0]
	if got.MCName != "" || got.BillingModel != "" || got.ServiceLevel != "" || got.SubscriptionStatus != "" {
		t.Errorf("nullable fields should be empty, got MCName=%q BillingModel=%q ServiceLevel=%q SubscriptionStatus=%q",
			got.MCName, got.BillingModel, got.ServiceLevel, got.SubscriptionStatus)
	}
}

func TestBuild_HierarchyCompleteNil(t *testing.T) {
	// When HIERARCHY_COMPLETE is NULL in the mart, it should scan as nil *bool
	// and HierarchyComplete should default to false.
	row := makeWorkerRow(
		"ocm-nil", "cluster-nil", "worker-nil", "111111111111",
		"ready", "us-east-1", "OrgX", "HCP", "hcp_worker", false, "mart",
		"mc", "mc-f", "222222222222", "us-east-1", "ready",
		"sc", "sc-f", "333333333333", "us-east-1", "ready",
		"prod", "standard", "premium", "active",
	)
	// Overwrite the hierarchyComplete position (index 9) with nil to simulate NULL.
	row[9] = nil

	mq := &mockQueryer{rows: &mockRows{data: [][]any{row}}}
	report, err := Build(context.Background(), mq, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if report.Rows[0].HierarchyComplete {
		t.Error("expected HierarchyComplete = false when NULL")
	}
}

func TestClusterIDs_FiltersEmpty(t *testing.T) {
	report := Report{
		GeneratedAt: time.Now(),
		Rows: []HierarchyRow{
			{CustomerClusterID: "uuid-a"},
			{CustomerClusterID: ""},
			{CustomerClusterID: "uuid-b"},
		},
	}
	ids := report.ClusterIDs()
	if len(ids) != 2 {
		t.Fatalf("expected 2 IDs, got %d", len(ids))
	}
	if ids[0] != "uuid-a" || ids[1] != "uuid-b" {
		t.Errorf("IDs = %v, want [uuid-a uuid-b]", ids)
	}
}

func TestClusterIDs_Empty(t *testing.T) {
	report := Report{}
	if ids := report.ClusterIDs(); len(ids) != 0 {
		t.Errorf("expected 0 IDs from empty report, got %d", len(ids))
	}
}

func TestMCAccountTargets_Deduplicates(t *testing.T) {
	report := Report{
		Rows: []HierarchyRow{
			{MCAccountID: "111111111111", MCRegion: "us-east-1"},
			{MCAccountID: "111111111111", MCRegion: "us-east-1"}, // duplicate pair
			{MCAccountID: "111111111111", MCRegion: "us-west-2"}, // same account, different region
			{MCAccountID: "222222222222", MCRegion: "eu-west-1"},
			{MCAccountID: "", MCRegion: "us-east-1"}, // empty — excluded
		},
	}
	targets := report.MCAccountTargets(aws.Config{})
	if len(targets) != 3 {
		t.Fatalf("expected 3 MC targets (deduplicated by account+region), got %d", len(targets))
	}
}

func TestSCAccountTargets_Deduplicates(t *testing.T) {
	report := Report{
		Rows: []HierarchyRow{
			{SCAccountID: "333333333333", SCRegion: "us-east-1"},
			{SCAccountID: "333333333333", SCRegion: "us-east-1"}, // duplicate
		},
	}
	targets := report.SCAccountTargets(aws.Config{})
	if len(targets) != 1 {
		t.Fatalf("expected 1 SC target, got %d", len(targets))
	}
	if targets[0].AccountID != "333333333333" {
		t.Errorf("AccountID = %q, want 333333333333", targets[0].AccountID)
	}
}

func TestAccountTargets_RegionSetOnConfig(t *testing.T) {
	report := Report{
		Rows: []HierarchyRow{
			{MCAccountID: "111111111111", MCRegion: "eu-central-1"},
		},
	}
	targets := report.MCAccountTargets(aws.Config{Region: "us-east-1"})
	if len(targets) != 1 {
		t.Fatalf("expected 1 target")
	}
	if targets[0].AWSConfig.Region != "eu-central-1" {
		t.Errorf("region = %q, want eu-central-1 (overrides base config)", targets[0].AWSConfig.Region)
	}
}

func TestBuildMartSQL_SelfJoin(t *testing.T) {
	sql := buildMartSQL("MY_DB.SCHEMA.TABLE")
	for _, want := range []string{
		"MY_DB.SCHEMA.TABLE w",
		"LEFT JOIN MY_DB.SCHEMA.TABLE mc ON w.HCP_MC_FLEET_ID = mc.MC_FLEET_ID",
		"LEFT JOIN MY_DB.SCHEMA.TABLE sc ON w.HCP_SC_FLEET_ID = sc.SC_FLEET_ID",
		"w.CLUSTER_ROLE   = 'worker'",
		"w.CLOUD_PROVIDER = 'aws'",
		"w.EXTERNAL_ID                   AS customer_cluster_id",
		"mc.AWS_ACCOUNT_ID               AS mc_account_id",
		"sc.AWS_ACCOUNT_ID               AS sc_account_id",
	} {
		if !strings.Contains(sql, want) {
			t.Errorf("SQL missing: %q", want)
		}
	}
}

func TestBuildMartSQL_DefaultView(t *testing.T) {
	sql := buildMartSQL(defaultMartView)
	if !strings.Contains(sql, defaultMartView) {
		t.Errorf("SQL should reference defaultMartView %q", defaultMartView)
	}
}
