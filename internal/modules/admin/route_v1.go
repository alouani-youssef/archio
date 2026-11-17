package admin

import (
	"net/http"

	"archio/internal/ent"
)

// RegisterRoutes mounts the admin endpoints, each wrapped in basic.
func RegisterRoutes(mux *http.ServeMux, client *ent.Client, basic func(http.Handler) http.Handler) {
	c := &controller{svc: &service{repo: &repository{client: client}}}
	route := func(pattern string, h http.HandlerFunc) { mux.Handle(pattern, basic(h)) }
	route("POST /v1/admin/groups", c.createGroup)
	route("GET /v1/admin/groups", c.listGroups)
	route("DELETE /v1/admin/groups/{groupId}", c.deleteGroup)
	route("POST /v1/admin/groups/{groupId}/keys", c.createKey)
	route("GET /v1/admin/groups/{groupId}/keys", c.listKeys)
	route("DELETE /v1/admin/groups/{groupId}/keys/{keyId}", c.revokeKey)
}
