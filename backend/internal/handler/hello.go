package handler

import (
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
