package handler

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/openshift-online/finops-tools/core/awsaccounts"

	backendsnowflake "github.com/openshift-online/finops-tools/backend/internal/snowflake"
	coresnowflake "github.com/openshift-online/finops-tools/core/snowflake"
)

func TestLivezHandler(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/livez", nil)
	rec := httptest.NewRecorder()

	(&Livez{}).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var body map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["status"] != "ok" {
		t.Fatalf("status = %q, want ok", body["status"])
	}
}

type fakeSnowflakeChecker struct {
	err error
}

func (f *fakeSnowflakeChecker) Check(context.Context) error {
	return f.err
}

func TestReadyzHandler(t *testing.T) {
	t.Run("no snowflake", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
		rec := httptest.NewRecorder()
		(&Readyz{}).ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
		}
	})

	t.Run("snowflake ok", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
		rec := httptest.NewRecorder()
		(&Readyz{Snowflake: &fakeSnowflakeChecker{}}).ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
		}
	})

	t.Run("snowflake unavailable", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
		rec := httptest.NewRecorder()
		(&Readyz{Snowflake: &fakeSnowflakeChecker{err: backendsnowflake.ErrUnavailable}}).ServeHTTP(rec, req)
		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusServiceUnavailable)
		}
	})
}

func TestOpenAPIHandler(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/openapi.yaml", nil)
	rec := httptest.NewRecorder()

	(&OpenAPI{}).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/yaml" {
		t.Fatalf("Content-Type = %q, want application/yaml", ct)
	}
	body := rec.Body.String()
	if body == "" {
		t.Fatal("expected non-empty OpenAPI document")
	}
	if !strings.Contains(body, "openapi: 3.0.3") {
		t.Fatalf("body missing openapi version")
	}
	if !strings.Contains(body, "/v1/aws/accounts/historical-count") {
		t.Fatalf("body missing historical-count path")
	}
}

type fakeQuerier struct {
	resp           backendsnowflake.QueryResponse
	err            error
	lastSQL        string
	lastConnection string
}

func (f *fakeQuerier) Query(_ context.Context, connection, sqlText string) (backendsnowflake.QueryResponse, error) {
	f.lastConnection = connection
	f.lastSQL = sqlText
	return f.resp, f.err
}

func TestSnowflakeQueryHandler(t *testing.T) {
	q := &fakeQuerier{
		resp: backendsnowflake.QueryResponse{
			Result: coresnowflake.QueryResult{
				Columns: []string{"N"},
				Rows:    [][]string{{"1"}},
			},
		},
	}
	h := &SnowflakeQuery{Querier: q}

	body := bytes.NewBufferString(`{"sql":"SELECT 1"}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/snowflake/query", body)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if q.lastSQL != "SELECT 1" {
		t.Fatalf("sql = %q, want SELECT 1", q.lastSQL)
	}
	if q.lastConnection != "" {
		t.Fatalf("connection = %q, want empty default", q.lastConnection)
	}

	var resp snowflakeQueryResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.RowCount != 1 || resp.Truncated {
		t.Fatalf("unexpected response: %+v", resp)
	}
}

func TestSnowflakeQueryConnectionRouting(t *testing.T) {
	q := &fakeQuerier{
		resp: backendsnowflake.QueryResponse{
			Result: coresnowflake.QueryResult{
				Columns: []string{"N"},
				Rows:    [][]string{{"1"}},
			},
		},
	}
	h := &SnowflakeQuery{Querier: q}

	t.Run("json connection field", func(t *testing.T) {
		q.lastConnection = ""
		body := bytes.NewBufferString(`{"connection":"sandbox","sql":"SELECT 1"}`)
		req := httptest.NewRequest(http.MethodPost, "/v1/snowflake/query", body)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
		}
		if q.lastConnection != "sandbox" {
			t.Fatalf("connection = %q, want sandbox", q.lastConnection)
		}
	})

	t.Run("header fallback", func(t *testing.T) {
		q.lastConnection = ""
		req := httptest.NewRequest(http.MethodPost, "/v1/snowflake/query", bytes.NewBufferString(`{"sql":"SELECT 1"}`))
		req.Header.Set("X-FinOps-Snowflake-Connection", "prod")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
		}
		if q.lastConnection != "prod" {
			t.Fatalf("connection = %q, want prod", q.lastConnection)
		}
	})

	t.Run("unknown connection", func(t *testing.T) {
		q.err = backendsnowflake.ErrUnknownConnection
		req := httptest.NewRequest(http.MethodPost, "/v1/snowflake/query", bytes.NewBufferString(`{"connection":"missing","sql":"SELECT 1"}`))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
		}
		q.err = nil
	})
}

func TestSnowflakeQueryValidation(t *testing.T) {
	h := &SnowflakeQuery{Querier: &fakeQuerier{}}

	tests := []struct {
		name   string
		body   string
		status int
	}{
		{name: "empty sql", body: `{}`, status: http.StatusBadRequest},
		{name: "multi statement", body: `{"sql":"SELECT 1; SELECT 2"}`, status: http.StatusBadRequest},
		{name: "trailing semicolon ok", body: `{"sql":"SELECT 1;"}`, status: http.StatusOK},
		{name: "delete rejected", body: `{"sql":"DELETE FROM t"}`, status: http.StatusBadRequest},
		{name: "insert rejected", body: `{"sql":"INSERT INTO t VALUES (1)"}`, status: http.StatusBadRequest},
		{name: "with select ok", body: `{"sql":"WITH cte AS (SELECT 1) SELECT * FROM cte"}`, status: http.StatusOK},
		{name: "body too large", body: `{"sql":"` + strings.Repeat("x", 1<<20) + `"}`, status: http.StatusRequestEntityTooLarge},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/v1/snowflake/query", bytes.NewBufferString(tc.body))
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != tc.status {
				t.Fatalf("status = %d, want %d, body = %s", rec.Code, tc.status, rec.Body.String())
			}
		})
	}
}

func TestSnowflakeQueryNotConfigured(t *testing.T) {
	h := &SnowflakeQuery{}
	req := httptest.NewRequest(http.MethodPost, "/v1/snowflake/query", bytes.NewBufferString(`{"sql":"SELECT 1"}`))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusServiceUnavailable)
	}
}

func TestValidateSQL(t *testing.T) {
	tests := []struct {
		name    string
		sql     string
		want    string
		wantErr bool
	}{
		{name: "select trimmed", sql: " SELECT 1 ; ", want: "SELECT 1"},
		{name: "with cte", sql: "WITH cte AS (SELECT 1) SELECT * FROM cte", want: "WITH cte AS (SELECT 1) SELECT * FROM cte"},
		{name: "show", sql: "SHOW TABLES", want: "SHOW TABLES"},
		{name: "describe", sql: "DESCRIBE TABLE t", want: "DESCRIBE TABLE t"},
		{name: "explain", sql: "EXPLAIN SELECT 1", want: "EXPLAIN SELECT 1"},
		{name: "leading comment", sql: "-- setup\nSELECT 1", want: "-- setup\nSELECT 1"},
		{name: "delete", sql: "DELETE FROM t", wantErr: true},
		{name: "create", sql: "CREATE TABLE t (id INT)", wantErr: true},
		{name: "comment bypass", sql: "/*x*/ DELETE FROM t", wantErr: true},
		{name: "with insert", sql: "WITH cte AS (SELECT 1) INSERT INTO t SELECT * FROM cte", wantErr: true},
		{name: "with update", sql: "WITH cte AS (SELECT 1) UPDATE t SET col = 1 WHERE id IN (SELECT 1 FROM cte)", wantErr: true},
		{name: "with delete", sql: "WITH cte AS (SELECT 1) DELETE FROM t WHERE id IN (SELECT 1 FROM cte)", wantErr: true},
		{name: "with merge", sql: "WITH cte AS (SELECT 1 AS id) MERGE INTO t USING cte ON t.id = cte.id WHEN MATCHED THEN DELETE", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := validateSQL(tc.sql)
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("validateSQL: %v", err)
			}
			if got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

type fakeAWSAccountsDB struct {
	db         *sql.DB
	err        error
	connection string
}

func (f *fakeAWSAccountsDB) Database(_ context.Context, connection string) (*sql.DB, error) {
	f.connection = connection
	if f.err != nil {
		return nil, f.err
	}
	return f.db, nil
}

func TestAWSAccountsHistoricalCountHandler(t *testing.T) {
	fakeDB := &fakeAWSAccountsDB{db: nil}
	h := &AWSAccountsHistoricalCount{
		Querier: fakeDB,
		Table:   "HCMFINOPSSOURCE_DB.MARTS.AWS_ACCOUNTS_HISTORICAL_COUNT",
		MaxRows: 10000,
		QueryFn: func(_ context.Context, _ awsaccounts.RowQuerier, _ awsaccounts.QueryOptions) (awsaccounts.QueryResult, error) {
			return awsaccounts.QueryResult{
				Points: []awsaccounts.DailyPoint{
					{
						Date:              "2026-01-19",
						PayerAccountID:    "123456789012",
						NBActiveAccounts:  100,
						NBClosedAccounts:  1,
						NBDeletedAccounts: 0,
					},
				},
			}, nil
		},
	}

	req := httptest.NewRequest(http.MethodGet, "/v1/aws/accounts/historical-count?payer_account_id=123456789012&from=2026-01-01&to=2026-01-31", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	var resp awsAccountsHistoricalCountResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Aggregate != awsaccounts.AggregatePayer || resp.RowCount != 1 || resp.Truncated {
		t.Fatalf("unexpected response: %+v", resp)
	}
	if resp.Data[0].PayerAccountID != "123456789012" || resp.Data[0].NBActiveAccounts != 100 {
		t.Fatalf("unexpected data point: %+v", resp.Data[0])
	}
}

func TestAWSAccountsHistoricalCountValidation(t *testing.T) {
	h := &AWSAccountsHistoricalCount{
		Querier: &fakeAWSAccountsDB{},
		Table:   "HCMFINOPSSOURCE_DB.MARTS.AWS_ACCOUNTS_HISTORICAL_COUNT",
	}

	req := httptest.NewRequest(http.MethodGet, "/v1/aws/accounts/historical-count?payer_account_id=bad", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
}

func TestAWSAccountsHistoricalCountNotConfigured(t *testing.T) {
	h := &AWSAccountsHistoricalCount{
		Table: "HCMFINOPSSOURCE_DB.MARTS.AWS_ACCOUNTS_HISTORICAL_COUNT",
	}
	req := httptest.NewRequest(http.MethodGet, "/v1/aws/accounts/historical-count", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusServiceUnavailable)
	}
}

func TestAWSAccountsHistoricalCountUnknownConnection(t *testing.T) {
	h := &AWSAccountsHistoricalCount{
		Querier: &fakeAWSAccountsDB{err: backendsnowflake.ErrUnknownConnection},
		Table:   "HCMFINOPSSOURCE_DB.MARTS.AWS_ACCOUNTS_HISTORICAL_COUNT",
	}
	req := httptest.NewRequest(http.MethodGet, "/v1/aws/accounts/historical-count?connection=missing", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

