package handler

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/openshift-online/finops-tools/core/awsaccounts"

	backendsnowflake "github.com/openshift-online/finops-tools/backend/internal/snowflake"
)

// AWSAccountsHistoricalQuerier provides Snowflake database access for account history queries.
type AWSAccountsHistoricalQuerier interface {
	Database(ctx context.Context, connection string) (*sql.DB, error)
}

// AWSAccountsHistoricalCount serves GET /v1/aws/accounts/historical-count.
type AWSAccountsHistoricalCount struct {
	Querier      AWSAccountsHistoricalQuerier
	Table        string
	MaxRows      int
	QueryTimeout time.Duration
	QueryFn      func(context.Context, awsaccounts.RowQuerier, awsaccounts.QueryOptions) (awsaccounts.QueryResult, error)
}

type awsAccountsHistoricalCountPoint struct {
	Date              string `json:"date"`
	PayerAccountID    string `json:"payer_account_id,omitempty"`
	NBActiveAccounts  int64  `json:"nb_active_accounts"`
	NBClosedAccounts  int64  `json:"nb_closed_accounts"`
	NBDeletedAccounts int64  `json:"nb_deleted_accounts"`
}

type awsAccountsHistoricalCountResponse struct {
	Aggregate string                              `json:"aggregate"`
	From      string                              `json:"from,omitempty"`
	To        string                              `json:"to,omitempty"`
	Data      []awsAccountsHistoricalCountPoint   `json:"data"`
	RowCount  int                                  `json:"row_count"`
	Truncated bool                                 `json:"truncated"`
}

func (h *AWSAccountsHistoricalCount) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if h.Querier == nil {
		writeError(w, http.StatusServiceUnavailable, "snowflake is not configured")
		return
	}

	query := r.URL.Query()
	fromRaw := strings.TrimSpace(query.Get("from"))
	toRaw := strings.TrimSpace(query.Get("to"))
	payerAccountID := strings.TrimSpace(query.Get("payer_account_id"))
	aggregate := strings.TrimSpace(query.Get("aggregate"))
	connection := strings.TrimSpace(query.Get("connection"))
	if connection == "" {
		connection = strings.TrimSpace(r.Header.Get(snowflakeConnectionHeader))
	}

	from, err := awsaccounts.ParseDate(fromRaw)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	to, err := awsaccounts.ParseDate(toRaw)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	opts := awsaccounts.QueryOptions{
		Table:          h.Table,
		PayerAccountID: payerAccountID,
		From:           from,
		To:             to,
		Aggregate:      aggregate,
		MaxRows:        h.MaxRows,
	}
	opts, err = awsaccounts.ValidateQueryOptions(opts)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	timeout := h.QueryTimeout
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()

	db, err := h.Querier.Database(ctx, connection)
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

	queryFn := h.QueryFn
	if queryFn == nil {
		queryFn = awsaccounts.QueryHistoricalCount
	}

	result, err := queryFn(ctx, awsaccounts.DBQuerier{DB: db}, opts)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
			writeError(w, http.StatusGatewayTimeout, "query timed out")
			return
		}
		slog.Error("aws accounts historical count query failed", "error", err, "connection", connection)
		writeError(w, http.StatusBadGateway, "snowflake query failed")
		return
	}

	writeJSON(w, http.StatusOK, awsAccountsHistoricalCountResponse{
		Aggregate: opts.Aggregate,
		From:      fromRaw,
		To:        toRaw,
		Data:      mapHistoricalCountPoints(result.Points, opts.Aggregate),
		RowCount:  len(result.Points),
		Truncated: result.Truncated,
	})
}

func mapHistoricalCountPoints(points []awsaccounts.DailyPoint, aggregate string) []awsAccountsHistoricalCountPoint {
	out := make([]awsAccountsHistoricalCountPoint, 0, len(points))
	for _, point := range points {
		item := awsAccountsHistoricalCountPoint{
			Date:              point.Date,
			NBActiveAccounts:  point.NBActiveAccounts,
			NBClosedAccounts:  point.NBClosedAccounts,
			NBDeletedAccounts: point.NBDeletedAccounts,
		}
		if aggregate == awsaccounts.AggregatePayer {
			item.PayerAccountID = point.PayerAccountID
		}
		out = append(out, item)
	}
	return out
}
