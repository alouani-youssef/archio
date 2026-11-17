package server

import "net/http"

// NewMux mounts the health handler, then lets each registrar mount its routes.
func NewMux(healthHandler http.Handler, registrars ...func(*http.ServeMux)) *http.ServeMux {
	mux := http.NewServeMux()
	if healthHandler != nil {
		mux.Handle("GET /health", healthHandler)
	}
	for _, register := range registrars {
		register(mux)
	}
	return mux
}
