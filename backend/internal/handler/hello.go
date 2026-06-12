package handler

import (
	"encoding/json"
	"net/http"

	"github.com/openshift-online/finops-tools/core"
)

// Hello serves GET /hello.
type Hello struct{}

type helloResponse struct {
	Message string `json:"message"`
}

func (h *Hello) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	msg, err := core.Hello(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "hello failed")
		return
	}

	writeJSON(w, http.StatusOK, helloResponse{Message: msg})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
