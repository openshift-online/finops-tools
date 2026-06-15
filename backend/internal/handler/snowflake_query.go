package handler

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	backendsnowflake "github.com/openshift-online/finops-tools/backend/internal/snowflake"
)

const snowflakeConnectionHeader = "X-FinOps-Snowflake-Connection"

// SnowflakeQuerier executes SQL against Snowflake.
type SnowflakeQuerier interface {
	Query(ctx context.Context, connection, sqlText string) (backendsnowflake.QueryResponse, error)
}

// SnowflakeQuery serves POST /v1/snowflake/query.
type SnowflakeQuery struct {
	Querier      SnowflakeQuerier
	QueryTimeout time.Duration
}

type snowflakeQueryRequest struct {
	SQL        string `json:"sql"`
	Connection string `json:"connection,omitempty"`
}

type snowflakeQueryResponse struct {
	Columns   []string   `json:"columns"`
	Rows      [][]string `json:"rows"`
	RowCount  int        `json:"row_count"`
	Truncated bool       `json:"truncated"`
}

func (h *SnowflakeQuery) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if h.Querier == nil {
		writeError(w, http.StatusServiceUnavailable, "snowflake is not configured")
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeError(w, http.StatusBadRequest, "read request body")
		return
	}

	var req snowflakeQueryRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	sqlText, err := validateSQL(req.SQL)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	connection := strings.TrimSpace(req.Connection)
	if connection == "" {
		connection = strings.TrimSpace(r.Header.Get(snowflakeConnectionHeader))
	}

	timeout := h.QueryTimeout
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()

	resp, err := h.Querier.Query(ctx, connection, sqlText)
	if err != nil {
		if errors.Is(err, backendsnowflake.ErrUnknownConnection) {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if errors.Is(err, backendsnowflake.ErrUnavailable) {
			slog.Error("snowflake unavailable", "error", err, "connection", connection)
			writeError(w, http.StatusServiceUnavailable, "snowflake is not available")
			return
		}
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
			writeError(w, http.StatusGatewayTimeout, "query timed out")
			return
		}
		writeError(w, http.StatusBadGateway, "snowflake query failed")
		return
	}

	writeJSON(w, http.StatusOK, snowflakeQueryResponse{
		Columns:   resp.Result.Columns,
		Rows:      resp.Result.Rows,
		RowCount:  len(resp.Result.Rows),
		Truncated: resp.Truncated,
	})
}

func validateSQL(sqlText string) (string, error) {
	trimmed := strings.TrimSpace(sqlText)
	if trimmed == "" {
		return "", errors.New("sql is required")
	}
	for strings.HasSuffix(trimmed, ";") {
		trimmed = strings.TrimSpace(strings.TrimSuffix(trimmed, ";"))
	}
	if trimmed == "" {
		return "", errors.New("sql is required")
	}
	if strings.Contains(trimmed, ";") {
		return "", errors.New("multi-statement SQL is not allowed")
	}
	return trimmed, nil
}
