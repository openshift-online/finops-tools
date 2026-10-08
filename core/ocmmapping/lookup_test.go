package ocmmapping

import (
	"context"
	"strings"
	"testing"

	"github.com/openshift-online/finops-tools/core/sqlrows"
)

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
		p, ok := d.(*string)
		if !ok {
			continue
		}
		if row[i] == nil {
			*p = ""
			continue
		}
		if s, ok := row[i].(string); ok {
			*p = s
		}
	}
	return nil
}

type mockQueryer struct {
	rows      *mockRows
	err       error
	lastQuery string
	lastArgs  []any
}

func (m *mockQueryer) QueryContext(_ context.Context, query string, args ...any) (sqlrows.Rows, error) {
	m.lastQuery = query
	m.lastArgs = append([]any(nil), args...)
	if m.err != nil {
		return nil, m.err
	}
	return m.rows, nil
}

func TestLookupByAWSAccounts_GroupsByAccountAndPreservesEnvironment(t *testing.T) {
	t.Parallel()
	mq := &mockQueryer{rows: &mockRows{data: [][]any{
		{"Production", "prod-cluster", "ocm-1", "ROSA Classic", "ready", "us-east-1", "4.16.0", "111111111111"},
		{"Stage", "stage-cluster", "ocm-2", "OSD", "ready", "us-west-2", "4.15.0", "111111111111"},
		{"Integration", "int-cluster", "ocm-3", "ROSA HCP", "installing", "eu-west-1", "4.17.0", "222222222222"},
	}}}

	got, err := LookupByAWSAccounts(context.Background(), mq, []string{"111111111111", "222222222222", "111111111111"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(got["111111111111"]) != 2 {
		t.Fatalf("account 111111111111 clusters = %d, want 2", len(got["111111111111"]))
	}
	if got["111111111111"][0].Environment != "Production" || got["111111111111"][1].Environment != "Stage" {
		t.Fatalf("environments = %q, %q", got["111111111111"][0].Environment, got["111111111111"][1].Environment)
	}
	if len(got["222222222222"]) != 1 || got["222222222222"][0].ClusterID != "ocm-3" {
		t.Fatalf("account 222222222222 = %+v", got["222222222222"])
	}
	if !strings.Contains(mq.lastQuery, "HCMFINOPS_DB.MARTS.OCM_MAPPING") {
		t.Fatalf("query missing default table: %s", mq.lastQuery)
	}
	if !strings.Contains(mq.lastQuery, "LOWER(CLOUD_PROVIDER) = 'aws'") {
		t.Fatalf("query missing aws filter: %s", mq.lastQuery)
	}
	if !strings.Contains(mq.lastQuery, "AWS_ACCOUNT_ID IN (?, ?)") {
		t.Fatalf("query missing IN placeholders: %s", mq.lastQuery)
	}
	if len(mq.lastArgs) != 2 || mq.lastArgs[0] != "111111111111" || mq.lastArgs[1] != "222222222222" {
		t.Fatalf("args = %#v", mq.lastArgs)
	}
}

func TestLookupByAWSAccounts_EmptyAccounts(t *testing.T) {
	t.Parallel()
	got, err := LookupByAWSAccounts(context.Background(), &mockQueryer{}, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("got = %#v", got)
	}
}

func TestLookupByAWSAccounts_InvalidTable(t *testing.T) {
	t.Parallel()
	_, err := LookupByAWSAccounts(context.Background(), &mockQueryer{rows: &mockRows{}}, []string{"111111111111"}, "bad;drop")
	if err == nil {
		t.Fatal("expected invalid table error")
	}
}

func TestBuildLookupSQL_CustomTable(t *testing.T) {
	t.Parallel()
	sqlText, args, err := buildLookupSQL("HCMFINOPS_DB.MARTS.OCM_MAPPING", []string{"111111111111"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sqlText, "FROM HCMFINOPS_DB.MARTS.OCM_MAPPING") {
		t.Fatalf("sql = %s", sqlText)
	}
	if len(args) != 1 || args[0] != "111111111111" {
		t.Fatalf("args = %#v", args)
	}
}
