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

type Dependencies struct {
	Postgres Checker
	Redis    Checker
	RabbitMQ Checker
}

type Handler struct {
	dependencies Dependencies
	timeout      time.Duration
}

type dependencyStatus struct {
	Postgres string `json:"postgres"`
	Redis    string `json:"redis"`
	RabbitMQ string `json:"rabbitmq"`
}

type response struct {
	Status       string           `json:"status"`
	Dependencies dependencyStatus `json:"dependencies"`
}

// NewHandler creates a readiness handler that checks every configured dependency.
func NewHandler(dependencies Dependencies) *Handler {
	return &Handler{dependencies: dependencies, timeout: 2 * time.Second}
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

	status := dependencyStatus{
		Postgres: check(ctx, h.dependencies.Postgres),
		Redis:    check(ctx, h.dependencies.Redis),
		RabbitMQ: check(ctx, h.dependencies.RabbitMQ),
	}
	if status.Postgres != "ok" || status.Redis != "ok" || status.RabbitMQ != "ok" {
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(response{Status: "not_ready", Dependencies: status})
		return
	}

	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(response{Status: "ok", Dependencies: status})
}

func check(ctx context.Context, checker Checker) string {
	if checker == nil || checker.Ping(ctx) != nil {
		return "unavailable"
	}
	return "ok"
}
