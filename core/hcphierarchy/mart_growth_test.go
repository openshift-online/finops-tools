package hcphierarchy

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// sequentialQueryer returns a different *mockRows (or error) for each
// successive QueryContext call, in call order. Used to test functions that
// issue more than one query per Build call (e.g. BuildClusterCounts).
type sequentialQueryer struct {
	calls   []string // captured query text, in call order
	results []*mockRows
	errs    []error
	index   int
}

func (m *sequentialQueryer) QueryContext(_ context.Context, query string, _ ...any) (SnowflakeRows, error) {
	m.calls = append(m.calls, query)
	i := m.index
	m.index++
	if i < len(m.errs) && m.errs[i] != nil {
		return nil, m.errs[i]
	}
	if i >= len(m.results) {
		return &mockRows{}, nil
	}
	return m.results[i], nil
}

func (m *sequentialQueryer) Close() error { return nil }

func TestBuildClusterCounts_ReturnsCountsAndActiveSnapshot(t *testing.T) {
	countsRows := &mockRows{data: [][]any{
		{"2026-01", "Production", int64(3)},
		{"2026-02", "Production", int64(5)},
		{"2026-01", "Stage", int64(1)},
	}}
	activeRows := &mockRows{data: [][]any{
		{"Production", int64(42)},
		{"Stage", int64(7)},
		{"Integration", int64(2)},
	}}
	q := &sequentialQueryer{results: []*mockRows{countsRows, activeRows}}

	report, err := BuildClusterCounts(context.Background(), q, "")
	if err != nil {
		t.Fatalf("BuildClusterCounts() error = %v", err)
	}

	if report.MartView != defaultMartView {
		t.Errorf("MartView = %q, want default %q", report.MartView, defaultMartView)
	}
	if len(report.Counts) != 3 {
		t.Fatalf("len(Counts) = %d, want 3", len(report.Counts))
	}
	want := ClusterCountRow{Month: "2026-02", Environment: "Production", NewClusters: 5}
	if report.Counts[1] != want {
		t.Errorf("Counts[1] = %+v, want %+v", report.Counts[1], want)
	}
	if len(report.ActiveByEnv) != 3 {
		t.Fatalf("len(ActiveByEnv) = %d, want 3", len(report.ActiveByEnv))
	}
	if report.ActiveByEnv[0] != (EnvironmentActiveCount{Environment: "Production", ActiveCount: 42}) {
		t.Errorf("ActiveByEnv[0] = %+v", report.ActiveByEnv[0])
	}

	if len(q.calls) != 2 {
		t.Fatalf("expected 2 queries issued, got %d", len(q.calls))
	}
	if !strings.Contains(q.calls[0], "new_this_month") || !strings.Contains(q.calls[0], "CREATION_TIMESTAMP") {
		t.Errorf("first query missing expected clauses: %s", q.calls[0])
	}
	if !strings.Contains(q.calls[1], "active_count") || !strings.Contains(q.calls[1], "'ready', 'hibernating'") {
		t.Errorf("second query missing expected clauses: %s", q.calls[1])
	}
}

func TestBuildClusterCounts_UsesProvidedMartView(t *testing.T) {
	q := &sequentialQueryer{results: []*mockRows{{}, {}}}
	custom := "HCMFINOPS_DB.MARTS.CUSTOM_MAPPING"

	report, err := BuildClusterCounts(context.Background(), q, custom)
	if err != nil {
		t.Fatalf("BuildClusterCounts() error = %v", err)
	}
	if report.MartView != custom {
		t.Errorf("MartView = %q, want %q", report.MartView, custom)
	}
	for i, call := range q.calls {
		if !strings.Contains(call, custom) {
			t.Errorf("call[%d] does not reference custom mart view: %s", i, call)
		}
	}
}

func TestBuildClusterCounts_RejectsInvalidMartView(t *testing.T) {
	q := &sequentialQueryer{}
	if _, err := BuildClusterCounts(context.Background(), q, "not a valid identifier; DROP TABLE x"); err == nil {
		t.Fatal("expected error for invalid mart view identifier")
	}
	if len(q.calls) != 0 {
		t.Errorf("expected no queries issued for invalid mart view, got %d", len(q.calls))
	}
}

func TestBuildClusterCounts_FirstQueryError(t *testing.T) {
	q := &sequentialQueryer{errs: []error{errors.New("boom")}}
	if _, err := BuildClusterCounts(context.Background(), q, ""); err == nil {
		t.Fatal("expected error propagated from first query")
	}
}

func TestBuildClusterCounts_SecondQueryError(t *testing.T) {
	q := &sequentialQueryer{
		results: []*mockRows{{}},
		errs:    []error{nil, errors.New("boom")},
	}
	if _, err := BuildClusterCounts(context.Background(), q, ""); err == nil {
		t.Fatal("expected error propagated from second query")
	}
}

func TestBuildClusterCounts_RowScanError(t *testing.T) {
	q := &sequentialQueryer{results: []*mockRows{
		{data: [][]any{{"2026-01", "Production", int64(3)}}, err: errors.New("iteration failed")},
	}}
	if _, err := BuildClusterCounts(context.Background(), q, ""); err == nil {
		t.Fatal("expected error propagated from rows.Err()")
	}
}

func TestBuildClusterGrowth_ReturnsRows(t *testing.T) {
	rows := &mockRows{data: [][]any{
		{"2026-01", "ROSA HCP", int64(10)},
		{"2026-02", "ROSA HCP", int64(4)},
		{"2026-01", "OSD", int64(2)},
	}}
	q := &sequentialQueryer{results: []*mockRows{rows}}

	report, err := BuildClusterGrowth(context.Background(), q, "")
	if err != nil {
		t.Fatalf("BuildClusterGrowth() error = %v", err)
	}
	if len(report.Rows) != 3 {
		t.Fatalf("len(Rows) = %d, want 3", len(report.Rows))
	}
	want := ClusterGrowthRow{Month: "2026-02", ProductType: "ROSA HCP", NewClusters: 4}
	if report.Rows[1] != want {
		t.Errorf("Rows[1] = %+v, want %+v", report.Rows[1], want)
	}
	if len(q.calls) != 1 {
		t.Fatalf("expected 1 query issued, got %d", len(q.calls))
	}
	if !strings.Contains(q.calls[0], "PRODUCT_TYPE") || !strings.Contains(q.calls[0], "'worker', 'standalone'") {
		t.Errorf("query missing expected clauses: %s", q.calls[0])
	}
}

func TestBuildClusterGrowth_RejectsInvalidMartView(t *testing.T) {
	q := &sequentialQueryer{}
	if _, err := BuildClusterGrowth(context.Background(), q, "; DROP TABLE x"); err == nil {
		t.Fatal("expected error for invalid mart view identifier")
	}
}

func TestBuildClusterGrowth_QueryError(t *testing.T) {
	q := &sequentialQueryer{errs: []error{errors.New("boom")}}
	if _, err := BuildClusterGrowth(context.Background(), q, ""); err == nil {
		t.Fatal("expected error propagated from query")
	}
}

func TestBuildClusterGrowth_RowScanError(t *testing.T) {
	q := &sequentialQueryer{results: []*mockRows{
		{data: [][]any{{"2026-01", "ROSA HCP", int64(10)}}, err: errors.New("iteration failed")},
	}}
	if _, err := BuildClusterGrowth(context.Background(), q, ""); err == nil {
		t.Fatal("expected error propagated from rows.Err()")
	}
}

func TestClusterCountsSQL_ContainsExpectedClauses(t *testing.T) {
	sql := clusterCountsSQL(defaultMartView)
	for _, want := range []string{
		"DATE_TRUNC('month', CREATION_TIMESTAMP)",
		"ENVIRONMENT",
		"COUNT(DISTINCT EXTERNAL_ID)",
		"CLUSTER_ROLE = 'worker'",
		"CREATION_TIMESTAMP IS NOT NULL",
		"GROUP BY 1, 2",
	} {
		if !strings.Contains(sql, want) {
			t.Errorf("clusterCountsSQL missing %q:\n%s", want, sql)
		}
	}
}

func TestActiveEnvironmentCountsSQL_ContainsExpectedClauses(t *testing.T) {
	sql := activeEnvironmentCountsSQL(defaultMartView)
	for _, want := range []string{
		"ENVIRONMENT",
		"COUNT(DISTINCT EXTERNAL_ID)",
		"CLUSTER_ROLE = 'worker'",
		"STATE IN ('ready', 'hibernating')",
	} {
		if !strings.Contains(sql, want) {
			t.Errorf("activeEnvironmentCountsSQL missing %q:\n%s", want, sql)
		}
	}
}

func TestClusterGrowthSQL_ContainsExpectedClauses(t *testing.T) {
	sql := clusterGrowthSQL(defaultMartView)
	for _, want := range []string{
		"DATE_TRUNC('month', CREATION_TIMESTAMP)",
		"PRODUCT_TYPE",
		"COUNT(DISTINCT EXTERNAL_ID)",
		"CLUSTER_ROLE IN ('worker', 'standalone')",
		"CREATION_TIMESTAMP IS NOT NULL",
	} {
		if !strings.Contains(sql, want) {
			t.Errorf("clusterGrowthSQL missing %q:\n%s", want, sql)
		}
	}
}
