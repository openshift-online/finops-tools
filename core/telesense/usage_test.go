package telesense

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

// mockRows simulates Snowflake row iteration over pre-loaded test data.
type mockRows struct {
	data  [][]any
	index int
	err   error
}

func (m *mockRows) Next() bool   { m.index++; return m.index <= len(m.data) }
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
			if row[i] != nil {
				*p = row[i].(string)
			}
		case **float64:
			if row[i] == nil {
				*p = nil
			} else {
				v := row[i].(float64)
				*p = &v
			}
		}
	}
	return nil
}

type capturedCall struct {
	query string
	args  []any
}

// mockQueryer returns one mockRows per QueryContext call, in order (useful
// for asserting batching behavior), and records every call's SQL and args.
type mockQueryer struct {
	rowsPerCall []*mockRows
	calls       []capturedCall
	err         error
}

func (m *mockQueryer) QueryContext(_ context.Context, query string, args ...any) (SnowflakeRows, error) {
	m.calls = append(m.calls, capturedCall{query: query, args: args})
	if m.err != nil {
		return nil, m.err
	}
	idx := len(m.calls) - 1
	if idx >= len(m.rowsPerCall) {
		return &mockRows{}, nil
	}
	return m.rowsPerCall[idx], nil
}

func (m *mockQueryer) Close() error { return nil }

func testWindow() (time.Time, time.Time) {
	return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
}

func TestBuild_ReturnsRows(t *testing.T) {
	mq := &mockQueryer{rowsPerCall: []*mockRows{{data: [][]any{
		{"cluster-1", "2026-01", 744.0},
		{"cluster-1", "2026-02", 672.0},
	}}}}
	start, end := testWindow()

	report, err := Build(context.Background(), mq, []string{"cluster-1"}, start, end, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(report.Rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(report.Rows))
	}
	if report.UsageView != defaultUsageView {
		t.Errorf("UsageView = %q, want %q", report.UsageView, defaultUsageView)
	}
	if report.GeneratedAt.IsZero() {
		t.Error("GeneratedAt should not be zero")
	}

	byMonth := report.ByClusterMonth()
	if got := byMonth["cluster-1"]["2026-01"]; got != 744.0 {
		t.Errorf("ByClusterMonth[cluster-1][2026-01] = %v, want 744.0", got)
	}
	if got := byMonth["cluster-1"]["2026-02"]; got != 672.0 {
		t.Errorf("ByClusterMonth[cluster-1][2026-02] = %v, want 672.0", got)
	}
}

func TestBuild_TreatsNullVCPUHoursAsZero(t *testing.T) {
	mq := &mockQueryer{rowsPerCall: []*mockRows{{data: [][]any{
		{"cluster-1", "2026-01", nil},
	}}}}
	start, end := testWindow()

	report, err := Build(context.Background(), mq, []string{"cluster-1"}, start, end, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(report.Rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(report.Rows))
	}
	if report.Rows[0].VCPUHours != 0 {
		t.Errorf("expected 0 vcpu hours for NULL, got %v", report.Rows[0].VCPUHours)
	}
}

func TestBuild_EmptyExternalIDs(t *testing.T) {
	mq := &mockQueryer{}
	start, end := testWindow()

	report, err := Build(context.Background(), mq, nil, start, end, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(report.Rows) != 0 {
		t.Errorf("expected 0 rows, got %d", len(report.Rows))
	}
	if len(mq.calls) != 0 {
		t.Errorf("expected no queries for an empty ID list, got %d", len(mq.calls))
	}
}

func TestBuild_BatchesLargeIDLists(t *testing.T) {
	// 4001 IDs should produce 2 query calls (4000 + 1), per the batchSize cap.
	ids := make([]string, 4001)
	for i := range ids {
		ids[i] = fmt.Sprintf("cluster-%04d", i)
	}
	mq := &mockQueryer{rowsPerCall: []*mockRows{{}, {}}}
	start, end := testWindow()

	_, err := Build(context.Background(), mq, ids, start, end, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(mq.calls) != 2 {
		t.Fatalf("expected 2 batched queries, got %d", len(mq.calls))
	}
	// batch sizes: 4000 IDs + 2 date args = 4002; 1 ID + 2 date args = 3
	if len(mq.calls[0].args) != 4002 {
		t.Errorf("first batch args = %d, want 4002", len(mq.calls[0].args))
	}
	if len(mq.calls[1].args) != 3 {
		t.Errorf("second batch args = %d, want 3", len(mq.calls[1].args))
	}
}

func TestBuild_DedupesTrimsAndDropsEmptyIDs(t *testing.T) {
	mq := &mockQueryer{rowsPerCall: []*mockRows{{}}}
	start, end := testWindow()

	_, err := Build(context.Background(), mq, []string{" cluster-1 ", "cluster-1", "", "  ", "cluster-2"}, start, end, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(mq.calls) != 1 {
		t.Fatalf("expected 1 query call, got %d", len(mq.calls))
	}
	// 2 dates + 2 deduplicated, trimmed IDs.
	if len(mq.calls[0].args) != 4 {
		t.Errorf("expected 4 args (2 dates + 2 ids), got %d: %v", len(mq.calls[0].args), mq.calls[0].args)
	}
}

func TestBuild_QueryError(t *testing.T) {
	mq := &mockQueryer{err: fmt.Errorf("access denied")}
	start, end := testWindow()

	_, err := Build(context.Background(), mq, []string{"cluster-1"}, start, end, "")
	if err == nil {
		t.Fatal("expected error from query failure, got nil")
	}
	if !strings.Contains(err.Error(), "ocp usage query") {
		t.Errorf("error = %v, want to mention ocp usage query", err)
	}
}

func TestBuild_RejectsInvertedWindow(t *testing.T) {
	mq := &mockQueryer{}
	start, end := testWindow()

	_, err := Build(context.Background(), mq, []string{"cluster-1"}, end, start, "")
	if err == nil {
		t.Fatal("expected error for inverted window")
	}
}

func TestBuild_RejectsInvalidUsageView(t *testing.T) {
	mq := &mockQueryer{}
	start, end := testWindow()

	_, err := Build(context.Background(), mq, []string{"cluster-1"}, start, end, "FOO; DROP TABLE bar")
	if err == nil {
		t.Fatal("expected error for invalid usage view")
	}
	if !strings.Contains(err.Error(), "invalid usage view") {
		t.Errorf("error = %v, want invalid usage view", err)
	}
}

func TestBuild_DefaultUsageView(t *testing.T) {
	mq := &mockQueryer{rowsPerCall: []*mockRows{{}}}
	start, end := testWindow()

	report, err := Build(context.Background(), mq, []string{"cluster-1"}, start, end, "CUSTOM_DB.SCHEMA.TABLE")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if report.UsageView != "CUSTOM_DB.SCHEMA.TABLE" {
		t.Errorf("UsageView = %q, want CUSTOM_DB.SCHEMA.TABLE", report.UsageView)
	}
}

func TestBuildUsageSQL_ContainsExpectedClauses(t *testing.T) {
	start, end := testWindow()
	query, args := buildUsageSQL(defaultUsageView, []string{"a", "b"}, start, end)

	for _, want := range []string{
		"FROM " + defaultUsageView,
		"CLUSTER_ID IN (?, ?)",
		"SUM(WORKER_VCPUS_AVG * 24)",
		"DATE_TRUNC('month', TELE_DATE)",
		"TELE_DATE >= ?",
		"TELE_DATE <  ?",
	} {
		if !strings.Contains(query, want) {
			t.Errorf("query missing %q:\n%s", want, query)
		}
	}
	if len(args) != 4 {
		t.Fatalf("expected 4 args, got %d: %v", len(args), args)
	}
	if args[0] != "2026-01-01" || args[1] != "2026-03-01" {
		t.Errorf("date args = %v, want [2026-01-01 2026-03-01]", args[:2])
	}
	if args[2] != "a" || args[3] != "b" {
		t.Errorf("id args = %v, want [a b]", args[2:])
	}
}

func TestReport_TotalVCPUHours(t *testing.T) {
	report := Report{Rows: []ClusterMonthUsage{
		{ClusterID: "a", Month: "2026-01", VCPUHours: 100},
		{ClusterID: "b", Month: "2026-01", VCPUHours: 50},
		{ClusterID: "a", Month: "2026-02", VCPUHours: 10},
	}}
	if got := report.TotalVCPUHours("2026-01"); got != 150 {
		t.Errorf("TotalVCPUHours(2026-01) = %v, want 150", got)
	}
	if got := report.TotalVCPUHours("2026-03"); got != 0 {
		t.Errorf("TotalVCPUHours(2026-03) = %v, want 0", got)
	}
}

func TestReport_ByClusterMonth_Empty(t *testing.T) {
	report := Report{}
	if m := report.ByClusterMonth(); len(m) != 0 {
		t.Errorf("expected empty map, got %v", m)
	}
}

func TestReport_ByClusterMonth_SumsDuplicateRows(t *testing.T) {
	// Defensive: if a caller ever double-fetches the same cluster/month (e.g.
	// overlapping batches from a bug), ByClusterMonth should sum rather than
	// silently overwrite, so the failure mode is "too high" not "wrong".
	report := Report{Rows: []ClusterMonthUsage{
		{ClusterID: "a", Month: "2026-01", VCPUHours: 100},
		{ClusterID: "a", Month: "2026-01", VCPUHours: 25},
	}}
	byMonth := report.ByClusterMonth()
	if got := byMonth["a"]["2026-01"]; got != 125 {
		t.Errorf("ByClusterMonth[a][2026-01] = %v, want 125", got)
	}
}
