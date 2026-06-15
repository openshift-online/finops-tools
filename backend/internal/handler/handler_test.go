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
