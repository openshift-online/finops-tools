package handler

import (
	"net/http"

	"github.com/openshift-online/finops-tools/backend/internal/openapi"
)

// OpenAPI serves GET /openapi.yaml.
type OpenAPI struct{}

func (h *OpenAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	w.Header().Set("Content-Type", "application/yaml")
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write(openapi.YAML); err != nil {
		return
	}
}
