package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	backendsnowflake "github.com/openshift-online/finops-tools/backend/internal/snowflake"
	coresnowflake "github.com/openshift-online/finops-tools/core/snowflake"
)

func TestHelloHandler(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/hello", nil)
	rec := httptest.NewRecorder()

	(&Hello{}).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var body map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["message"] != "hello" {
		t.Fatalf("message = %q, want hello", body["message"])
	}
}

type fakeQuerier struct {
	resp backendsnowflake.QueryResponse
	err  error
	last string
}

func (f *fakeQuerier) Query(_ context.Context, sqlText string) (backendsnowflake.QueryResponse, error) {
	f.last = sqlText
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
	if q.last != "SELECT 1" {
		t.Fatalf("sql = %q, want SELECT 1", q.last)
	}

	var resp snowflakeQueryResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.RowCount != 1 || resp.Truncated {
		t.Fatalf("unexpected response: %+v", resp)
	}
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
	got, err := validateSQL(" SELECT 1 ; ")
	if err != nil {
		t.Fatalf("validateSQL: %v", err)
	}
	if got != "SELECT 1" {
		t.Fatalf("got %q", got)
	}
}
