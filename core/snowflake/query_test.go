package snowflake

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"
)

func TestQueryLimitedRejectsNegativeMaxRows(t *testing.T) {
	t.Parallel()

	_, _, err := QueryLimited(context.Background(), nil, "SELECT 1", -1)
	if err == nil {
		t.Fatal("expected error for negative maxRows")
	}
	if !strings.Contains(err.Error(), "invalid maxRows: -1") {
		t.Fatalf("err = %v, want invalid maxRows", err)
	}
}

func TestResolveColumnNames(t *testing.T) {
	t.Parallel()

	names := resolveColumnNames(
		[]string{"", "ACCOUNT_ID"},
		[]*sql.ColumnType{nil, nil},
	)
	if names[0] != "COLUMN_1" {
		t.Fatalf("names[0] = %q, want COLUMN_1", names[0])
	}
	if names[1] != "ACCOUNT_ID" {
		t.Fatalf("names[1] = %q, want ACCOUNT_ID", names[1])
	}
}

func TestValueString(t *testing.T) {
	t.Parallel()

	ts := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	tests := []struct {
		in   any
		want string
	}{
		{nil, ""},
		{"alpha", "alpha"},
		{[]byte("beta"), "beta"},
		{true, "true"},
		{false, "false"},
		{ts, ts.Format(time.RFC3339Nano)},
		{42, "42"},
	}
	for _, tc := range tests {
		if got := valueString(tc.in); got != tc.want {
			t.Fatalf("valueString(%#v) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
