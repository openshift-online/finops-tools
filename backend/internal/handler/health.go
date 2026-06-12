package handler

import (
	"context"
	"net/http"
)

// SnowflakeChecker verifies Snowflake connectivity for readiness probes.
type SnowflakeChecker interface {
	Check(ctx context.Context) error
}

// Livez serves GET /livez. It confirms the process can serve HTTP and does not
// call external dependencies (suitable for liveness probes).
type Livez struct{}

func (h *Livez) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// Readyz serves GET /readyz. When Snowflake is configured it must be reachable
// before the handler returns 200 (suitable for readiness and startup probes).
type Readyz struct {
	Snowflake SnowflakeChecker
}

func (h *Readyz) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	if h.Snowflake != nil {
		if err := h.Snowflake.Check(r.Context()); err != nil {
			writeError(w, http.StatusServiceUnavailable, "snowflake unavailable")
			return
		}
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
