package server

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/openshift-online/finops-tools/backend/internal/config"
	"github.com/openshift-online/finops-tools/backend/internal/handler"
	backendsnowflake "github.com/openshift-online/finops-tools/backend/internal/snowflake"
)

// Server is the HTTP API server.
type Server struct {
	cfg       config.Config
	snowflake *backendsnowflake.Registry
	http      *http.Server
	logger    *slog.Logger
}

// New builds a Server from configuration. Snowflake connects lazily on the first query.
func New(cfg config.Config, logger *slog.Logger) (*Server, error) {
	if logger == nil {
		logger = slog.Default()
	}

	s := &Server{cfg: cfg, logger: logger}

	var querier handler.SnowflakeQuerier
	if cfg.Snowflake != nil {
		s.snowflake = backendsnowflake.NewRegistry(
			cfg.Snowflake.Default,
			cfg.Snowflake.Connections,
			cfg.MaxRows,
			logger,
		)
		querier = s.snowflake
		logger.Info("snowflake configured; connections deferred until first query",
			"default_connection", cfg.Snowflake.Default,
			"connections", len(cfg.Snowflake.Connections),
		)
	} else {
		logger.Warn("snowflake not configured; /v1/snowflake/query will return 503")
	}

	mux := http.NewServeMux()
	mux.Handle("/openapi.yaml", &handler.OpenAPI{})
	livez := &handler.Livez{}
	mux.Handle("/livez", livez)
	mux.Handle("/health", livez) // backwards-compatible alias for /livez
	var snowflakeChecker handler.SnowflakeChecker
	if s.snowflake != nil {
		snowflakeChecker = s.snowflake
	}
	mux.Handle("/readyz", &handler.Readyz{Snowflake: snowflakeChecker})
	mux.Handle("/v1/snowflake/query", &handler.SnowflakeQuery{
		Querier:      querier,
		QueryTimeout: cfg.QueryTimeout,
	})
	mux.Handle("/v1/aws/accounts/historical-count", &handler.AWSAccountsHistoricalCount{
		Querier:      s.snowflake,
		Table:        cfg.AWSAccountsHistoricalTable,
		MaxRows:      cfg.AWSAccountsHistoricalMaxRows,
		QueryTimeout: cfg.QueryTimeout,
	})

	readTimeout := 30 * time.Second
	writeTimeout := cfg.QueryTimeout + 15*time.Second
	if writeTimeout < readTimeout {
		writeTimeout = readTimeout
	}

	s.http = &http.Server{
		Addr:              cfg.Addr,
		Handler:           loggingMiddleware(logger, mux),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       readTimeout,
		WriteTimeout:      writeTimeout,
	}

	return s, nil
}

// ListenAndServe starts the HTTP server.
func (s *Server) ListenAndServe() error {
	s.logger.Info("starting HTTP server", "addr", s.cfg.Addr)
	return s.http.ListenAndServe()
}

// Shutdown gracefully stops the HTTP server and closes the Snowflake handle.
func (s *Server) Shutdown(ctx context.Context) error {
	err := s.http.Shutdown(ctx)
	if s.snowflake != nil {
		if closeErr := s.snowflake.Close(); closeErr != nil && err == nil {
			err = closeErr
		}
	}
	return err
}

func loggingMiddleware(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		if isProbePath(r.URL.Path) {
			return
		}
		logger.Info("request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", rec.status,
			"duration", time.Since(start),
		)
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func isProbePath(path string) bool {
	switch path {
	case "/livez", "/readyz", "/health":
		return true
	default:
		return false
	}
}
