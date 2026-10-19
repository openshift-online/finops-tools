package handler

import (
	"net/http"
)

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

// Readyz serves GET /readyz. It confirms the process can serve HTTP and does not
// call external dependencies (including Snowflake), so probes do not resume a
// warehouse. Query handlers return 503 when Snowflake is unavailable.
type Readyz struct{}

func (h *Readyz) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
