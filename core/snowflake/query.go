package snowflake

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// QueryResult is the outcome of a SQL query.
type QueryResult struct {
	Columns []string
	Rows    [][]string
}

// Query executes SQL and returns all rows as strings.
func Query(ctx context.Context, db *sql.DB, sqlText string) (QueryResult, error) {
	result, _, err := QueryLimited(ctx, db, sqlText, 0)
	return result, err
}

// QueryLimited executes SQL and returns up to maxRows rows. When maxRows is 0,
// all rows are returned. The second return value is true when additional rows
// were available beyond maxRows.
func QueryLimited(ctx context.Context, db *sql.DB, sqlText string, maxRows int) (out QueryResult, truncated bool, err error) {
	if maxRows < 0 {
		return out, false, fmt.Errorf("invalid maxRows: %d", maxRows)
	}
	rows, err := db.QueryContext(ctx, sqlText)
	if err != nil {
		return out, false, fmt.Errorf("snowflake query: %w", err)
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("close snowflake query rows: %w", closeErr)
		}
	}()

	cols, err := rows.Columns()
	if err != nil {
		return out, false, err
	}
	colTypes, err := rows.ColumnTypes()
	if err != nil {
		return out, false, err
	}

	out = QueryResult{Columns: resolveColumnNames(cols, colTypes)}

	for rows.Next() {
		if maxRows > 0 && len(out.Rows) >= maxRows {
			truncated = true
			break
		}
		row, scanErr := scanRowValues(rows, len(out.Columns))
		if scanErr != nil {
			return out, false, scanErr
		}
		out.Rows = append(out.Rows, row)
	}
	if iterErr := rows.Err(); iterErr != nil {
		return out, false, iterErr
	}
	return out, truncated, nil
}

func resolveColumnNames(cols []string, colTypes []*sql.ColumnType) []string {
	names := make([]string, len(cols))
	for i, col := range cols {
		name := strings.TrimSpace(col)
		if name == "" && i < len(colTypes) && colTypes[i] != nil {
			name = strings.TrimSpace(colTypes[i].Name())
		}
		if name == "" {
			name = fmt.Sprintf("COLUMN_%d", i+1)
		}
		names[i] = name
	}
	return names
}

func scanRowValues(rows *sql.Rows, n int) ([]string, error) {
	holders := make([]any, n)
	ptrs := make([]any, n)
	for i := range holders {
		ptrs[i] = &holders[i]
	}
	if err := rows.Scan(ptrs...); err != nil {
		return nil, err
	}

	out := make([]string, n)
	for i, v := range holders {
		out[i] = valueString(v)
	}
	return out, nil
}

func valueString(v any) string {
	if v == nil {
		return ""
	}
	switch x := v.(type) {
	case string:
		return x
	case []byte:
		return string(x)
	case time.Time:
		return x.Format(time.RFC3339Nano)
	case bool:
		if x {
			return "true"
		}
		return "false"
	default:
		return fmt.Sprint(x)
	}
}
