package health

import (
	"context"
	"encoding/json"
	"net/http"
	"time"
)

// Checker is the small readiness dependency required by the health module.
type Checker interface {
	Ping(context.Context) error
}

type Handler struct {
	checker Checker
	timeout time.Duration
}

type response struct {
	Status   string `json:"status"`
	Database string `json:"database"`
}

// NewHandler creates a readiness handler that checks its dependency on each request.
func NewHandler(checker Checker) *Handler {
	return &Handler{checker: checker, timeout: 2 * time.Second}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	ctx, cancel := context.WithTimeout(r.Context(), h.timeout)
	defer cancel()

	if h.checker == nil || h.checker.Ping(ctx) != nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(response{Status: "not_ready", Database: "unavailable"})
		return
	}

	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(response{Status: "ok", Database: "ok"})
}
