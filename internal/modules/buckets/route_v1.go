package buckets

import (
	"net/http"

	"archio/internal/ent"
)

// RegisterRoutes mounts the bucket endpoints, each wrapped in keyAuth.
func RegisterRoutes(mux *http.ServeMux, client *ent.Client, keyAuth func(http.Handler) http.Handler) {
	c := &controller{svc: &service{repo: &repository{client: client}}}
	route := func(pattern string, h http.HandlerFunc) { mux.Handle(pattern, keyAuth(h)) }
	route("POST /v1/buckets", c.create)
	route("GET /v1/buckets", c.list)
	route("GET /v1/buckets/{bucket}", c.get)
	route("DELETE /v1/buckets/{bucket}", c.delete)
}
