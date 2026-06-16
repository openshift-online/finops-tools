package awsaccounts

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"
)

const testTable = "HCMFINOPSSOURCE_DB.MARTS.AWS_ACCOUNTS_HISTORICAL_COUNT"

func TestValidateQueryOptions(t *testing.T) {
	from := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 1, 31, 0, 0, 0, 0, time.UTC)

	got, err := ValidateQueryOptions(QueryOptions{
		Table:          testTable,
		PayerAccountID: "123456789012",
		From:           from,
		To:             to,
		Aggregate:      AggregateSum,
		MaxRows:        100,
	})
	if err != nil {
		t.Fatalf("ValidateQueryOptions() error = %v", err)
	}
	if got.Aggregate != AggregateSum {
		t.Fatalf("Aggregate = %q, want sum", got.Aggregate)
	}

	_, err = ValidateQueryOptions(QueryOptions{Table: testTable, PayerAccountID: "bad"})
	if err == nil {
		t.Fatal("expected invalid payer error")
	}

	_, err = ValidateQueryOptions(QueryOptions{Table: "bad.table"})
	if err == nil {
		t.Fatal("expected invalid table error")
	}

	_, err = ValidateQueryOptions(QueryOptions{
		Table: testTable,
		From:  to,
		To:    from,
	})
	if err == nil {
		t.Fatal("expected from after to error")
	}
}

func TestParseDate(t *testing.T) {
	got, err := ParseDate("2026-01-19")
	if err != nil {
		t.Fatalf("ParseDate() error = %v", err)
	}
	if got.Format(dateOnlyLayout) != "2026-01-19" {
		t.Fatalf("date = %q", got.Format(dateOnlyLayout))
	}

	if _, err := ParseDate("01/19/2026"); err == nil {
		t.Fatal("expected parse error")
	}
}

func TestBuildHistoricalCountSQLPayerMode(t *testing.T) {
	sqlText, args, err := BuildHistoricalCountSQL(QueryOptions{
		Table:          testTable,
		PayerAccountID: "123456789012",
		From:           time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		To:             time.Date(2026, 1, 31, 0, 0, 0, 0, time.UTC),
		Aggregate:      AggregatePayer,
	})
	if err != nil {
		t.Fatalf("BuildHistoricalCountSQL() error = %v", err)
	}
	for _, want := range []string{
		"FROM " + testTable,
		"PAYER_ACCOUNT_ID = ?",
		"TIMESTAMP >= ?",
		"TIMESTAMP < ?",
		"QUALIFY ROW_NUMBER()",
		"PAYER_ACCOUNT_ID",
		"ORDER BY snapshot_date, PAYER_ACCOUNT_ID",
	} {
		if !strings.Contains(sqlText, want) {
			t.Fatalf("SQL missing %q:\n%s", want, sqlText)
		}
	}
	wantArgs := []any{"123456789012", "2026-01-01", "2026-02-01"}
	if len(args) != len(wantArgs) {
		t.Fatalf("args = %#v, want %#v", args, wantArgs)
	}
	for i := range wantArgs {
		if args[i] != wantArgs[i] {
			t.Fatalf("args[%d] = %#v, want %#v", i, args[i], wantArgs[i])
		}
	}
}

func TestBuildHistoricalCountSQLSumMode(t *testing.T) {
	sqlText, args, err := BuildHistoricalCountSQL(QueryOptions{
		Table:     testTable,
		Aggregate: AggregateSum,
	})
	if err != nil {
		t.Fatalf("BuildHistoricalCountSQL() error = %v", err)
	}
	for _, want := range []string{
		"SUM(NB_ACTIVE_ACCOUNTS)",
		"GROUP BY DATE(TIMESTAMP)",
		"ORDER BY snapshot_date",
	} {
		if !strings.Contains(sqlText, want) {
			t.Fatalf("SQL missing %q:\n%s", want, sqlText)
		}
	}
	if strings.Contains(sqlText, "SELECT\n  DATE(TIMESTAMP) AS snapshot_date,\n  PAYER_ACCOUNT_ID") {
		t.Fatalf("sum SQL should not select payer_account_id in outer query: %s", sqlText)
	}
	if len(args) != 0 {
		t.Fatalf("args = %#v, want none", args)
	}
}

type fakeRows struct {
	rows    [][]any
	index   int
	closed  bool
	scanErr error
}

func (f *fakeRows) Next() bool {
	if f.index >= len(f.rows) {
		return false
	}
	f.index++
	return true
}

func (f *fakeRows) Scan(dest ...any) error {
	if f.scanErr != nil {
		return f.scanErr
	}
	row := f.rows[f.index-1]
	for i, ptr := range dest {
		switch d := ptr.(type) {
		case *sql.NullTime:
			*d = row[i].(sql.NullTime)
		case *sql.NullString:
			*d = row[i].(sql.NullString)
		case *sql.NullInt64:
			*d = row[i].(sql.NullInt64)
		default:
			return fmt.Errorf("unsupported scan type %T", ptr)
		}
	}
	return nil
}

func (f *fakeRows) Close() error {
	f.closed = true
	return nil
}

func (f *fakeRows) Err() error { return nil }

type fakeQuerier struct {
	sqlText string
	rows    *fakeRows
	err     error
}

func (f *fakeQuerier) QueryContext(_ context.Context, query string, _ ...any) (RowIterator, error) {
	f.sqlText = query
	if f.err != nil {
		return nil, f.err
	}
	return f.rows, nil
}

func TestQueryHistoricalCountPayerMode(t *testing.T) {
	rows := &fakeRows{
		rows: [][]any{
			{
				sql.NullTime{Time: time.Date(2026, 1, 19, 0, 0, 0, 0, time.UTC), Valid: true},
				sql.NullString{String: "123456789012", Valid: true},
				sql.NullInt64{Int64: 100, Valid: true},
				sql.NullInt64{Int64: 1, Valid: true},
				sql.NullInt64{Int64: 0, Valid: true},
			},
		},
	}
	q := &fakeQuerier{rows: rows}

	result, err := QueryHistoricalCount(context.Background(), q, QueryOptions{
		Table:     testTable,
		Aggregate: AggregatePayer,
	})
	if err != nil {
		t.Fatalf("QueryHistoricalCount() error = %v", err)
	}
	if len(result.Points) != 1 {
		t.Fatalf("points = %d, want 1", len(result.Points))
	}
	p := result.Points[0]
	if p.Date != "2026-01-19" || p.PayerAccountID != "123456789012" || p.NBActiveAccounts != 100 {
		t.Fatalf("unexpected point: %+v", p)
	}
	if q.sqlText == "" {
		t.Fatal("expected SQL to be executed")
	}
}

func TestQueryHistoricalCountTruncated(t *testing.T) {
	rows := &fakeRows{
		rows: [][]any{
			{
				sql.NullTime{Time: time.Date(2026, 1, 19, 0, 0, 0, 0, time.UTC), Valid: true},
				sql.NullInt64{Int64: 100, Valid: true},
				sql.NullInt64{Int64: 1, Valid: true},
				sql.NullInt64{Int64: 0, Valid: true},
			},
			{
				sql.NullTime{Time: time.Date(2026, 1, 20, 0, 0, 0, 0, time.UTC), Valid: true},
				sql.NullInt64{Int64: 101, Valid: true},
				sql.NullInt64{Int64: 1, Valid: true},
				sql.NullInt64{Int64: 0, Valid: true},
			},
		},
	}
	q := &fakeQuerier{rows: rows}

	result, err := QueryHistoricalCount(context.Background(), q, QueryOptions{
		Table:     testTable,
		Aggregate: AggregateSum,
		MaxRows:   1,
	})
	if err != nil {
		t.Fatalf("QueryHistoricalCount() error = %v", err)
	}
	if len(result.Points) != 1 {
		t.Fatalf("points = %d, want 1", len(result.Points))
	}
	if !result.Truncated {
		t.Fatal("expected truncated result")
	}
}
