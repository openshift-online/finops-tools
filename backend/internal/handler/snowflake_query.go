package handler

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"
	"unicode"

	backendsnowflake "github.com/openshift-online/finops-tools/backend/internal/snowflake"
)

var allowedReadOnlySQL = map[string]struct{}{
	"SELECT":   {},
	"SHOW":     {},
	"DESCRIBE": {},
	"DESC":     {},
	"EXPLAIN":  {},
	"LIST":     {},
}

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

	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	defer r.Body.Close()

	var req snowflakeQueryRequest
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(&req); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			writeError(w, http.StatusRequestEntityTooLarge, "request body too large")
			return
		}
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
	keyword, err := effectiveStatementKeyword(trimmed)
	if err != nil {
		return "", err
	}
	if _, ok := allowedReadOnlySQL[keyword]; !ok {
		return "", errors.New("only read-only SQL is allowed (SELECT, WITH ... SELECT, SHOW, DESCRIBE, DESC, EXPLAIN, LIST)")
	}
	return trimmed, nil
}

// effectiveStatementKeyword returns the top-level statement keyword, skipping
// leading WITH ... AS (...) CTE definitions so "WITH cte AS (...) INSERT ..."
// is recognized as INSERT, not WITH.
func effectiveStatementKeyword(sqlText string) (string, error) {
	s := trimSQLPrefix(sqlText)
	if s == "" {
		return "", errors.New("sql is required")
	}

	keyword, rest, err := readSQLKeyword(s)
	if err != nil {
		return "", err
	}
	if keyword != "WITH" {
		return keyword, nil
	}

	rest, err = skipCTEDefinitions(rest)
	if err != nil {
		return "", err
	}
	return effectiveStatementKeyword(rest)
}

func trimSQLPrefix(s string) string {
	s = strings.TrimSpace(stripSQLComments(s))
	for len(s) > 0 {
		switch s[0] {
		case '(', ' ', '\t', '\n', '\r':
			s = strings.TrimSpace(s[1:])
			continue
		}
		break
	}
	return s
}

func readSQLKeyword(s string) (keyword, rest string, err error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", "", errors.New("sql is required")
	}

	end := 0
	for end < len(s) {
		r := rune(s[end])
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' {
			end++
			continue
		}
		break
	}
	if end == 0 {
		return "", "", errors.New("invalid SQL")
	}
	return strings.ToUpper(s[:end]), strings.TrimSpace(s[end:]), nil
}

func skipCTEDefinitions(s string) (string, error) {
	s = strings.TrimSpace(s)
	if kw, after, ok := matchSQLKeywordPrefix(s, "RECURSIVE"); ok {
		s = after
		_ = kw
	}

	for {
		var err error
		_, s, err = readSQLKeyword(s)
		if err != nil {
			return "", err
		}
		var ok bool
		_, s, ok = matchSQLKeywordPrefix(s, "AS")
		if !ok {
			return "", errors.New("invalid SQL")
		}
		s, err = skipBalancedParens(s)
		if err != nil {
			return "", err
		}
		s = strings.TrimSpace(s)
		if len(s) > 0 && s[0] == ',' {
			s = strings.TrimSpace(s[1:])
			continue
		}
		return s, nil
	}
}

func matchSQLKeywordPrefix(s, word string) (keyword, rest string, ok bool) {
	keyword, rest, err := readSQLKeyword(s)
	if err != nil {
		return "", s, false
	}
	if keyword != word {
		return "", s, false
	}
	return keyword, rest, true
}

func skipBalancedParens(s string) (string, error) {
	s = strings.TrimSpace(s)
	if len(s) == 0 || s[0] != '(' {
		return "", errors.New("invalid SQL")
	}

	depth := 0
	inSingle := false
	inDouble := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if inSingle {
			if c == '\'' {
				if i+1 < len(s) && s[i+1] == '\'' {
					i++
					continue
				}
				inSingle = false
			}
			continue
		}
		if inDouble {
			if c == '"' {
				if i+1 < len(s) && s[i+1] == '"' {
					i++
					continue
				}
				inDouble = false
			}
			continue
		}

		switch c {
		case '\'':
			inSingle = true
		case '"':
			inDouble = true
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return strings.TrimSpace(s[i+1:]), nil
			}
		}
	}
	return "", errors.New("invalid SQL")
}

func stripSQLComments(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		if i+1 < len(s) && s[i] == '-' && s[i+1] == '-' {
			i += 2
			for i < len(s) && s[i] != '\n' {
				i++
			}
			continue
		}
		if i+1 < len(s) && s[i] == '/' && s[i+1] == '*' {
			i += 2
			for i+1 < len(s) && !(s[i] == '*' && s[i+1] == '/') {
				i++
			}
			if i+1 < len(s) {
				i += 2
			}
			continue
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}
