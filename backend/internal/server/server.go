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
	cfg      config.Config
	snowflake *backendsnowflake.LazyService
	http     *http.Server
	logger   *slog.Logger
}

// New builds a Server from configuration. Snowflake connects lazily on the first query.
func New(cfg config.Config, logger *slog.Logger) (*Server, error) {
	if logger == nil {
		logger = slog.Default()
	}

	s := &Server{cfg: cfg, logger: logger}

	var querier handler.SnowflakeQuerier
	if cfg.Snowflake != nil {
		s.snowflake = backendsnowflake.NewLazyService(cfg.Snowflake.Connect, cfg.MaxRows, logger)
		querier = s.snowflake
		logger.Info("snowflake configured; connection deferred until first query")
	} else {
		logger.Warn("snowflake not configured; /v1/snowflake/query will return 503")
	}

	mux := http.NewServeMux()
	hello := &handler.Hello{}
	mux.Handle("/hello", hello)
	mux.Handle("/health", &handler.Health{Hello: hello})
	mux.Handle("/v1/snowflake/query", &handler.SnowflakeQuery{
		Querier:      querier,
		QueryTimeout: cfg.QueryTimeout,
	})

	s.http = &http.Server{
		Addr:              cfg.Addr,
		Handler:           loggingMiddleware(logger, mux),
		ReadHeaderTimeout: 10 * time.Second,
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
