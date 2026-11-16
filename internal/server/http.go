package server

import "net/http"

// NewMux mounts feature module handlers on the HTTP transport.
func NewMux(healthHandler http.Handler) *http.ServeMux {
	mux := http.NewServeMux()
	if healthHandler != nil {
		mux.Handle("GET /health", healthHandler)
	}
	return mux
}
