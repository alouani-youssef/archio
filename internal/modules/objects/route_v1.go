package objects

import (
	"net/http"

	"archio/internal/ent"
	"archio/internal/events"
	"archio/internal/storage"
)

// Deps are the dependencies of the object endpoints.
type Deps struct {
	Client         *ent.Client
	Storage        storage.Storage
	Publisher      events.Publisher
	PublicBaseURL  string
	MaxUploadBytes int64
}

// RegisterRoutes mounts the object endpoints. All are wrapped in keyAuth except
// the read URL, which is public.
func RegisterRoutes(mux *http.ServeMux, d Deps, keyAuth func(http.Handler) http.Handler) {
	c := &controller{
		svc:            &service{repo: &repository{client: d.Client}, storage: d.Storage, publisher: d.Publisher},
		publicBaseURL:  d.PublicBaseURL,
		maxUploadBytes: d.MaxUploadBytes,
	}
	route := func(pattern string, h http.HandlerFunc) { mux.Handle(pattern, keyAuth(h)) }
	route("PUT /v1/buckets/{bucket}/objects/{key...}", c.upload)
	route("GET /v1/buckets/{bucket}/objects", c.list)
	route("GET /v1/buckets/{bucket}/versions/{key...}", c.versions)
	route("DELETE /v1/buckets/{bucket}/objects/{key...}", c.delete)
	mux.HandleFunc("GET /v1/objects/{versionId}", c.read)
}
