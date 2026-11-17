package admin

import (
	"encoding/json"
	"net/http"

	"archio/internal/ent"
	apperr "archio/pkg/errors"
	"github.com/google/uuid"
)

type controller struct{ svc *service }

func toGroup(g *ent.Group) groupResponse {
	return groupResponse{ID: g.ID.String(), Name: g.Name, CreatedAt: g.CreatedAt}
}

func toKey(k *ent.ApiKey) keyResponse {
	return keyResponse{ID: k.ID.String(), Name: k.Name, Prefix: k.Prefix,
		CreatedAt: k.CreatedAt, LastUsedAt: k.LastUsedAt, RevokedAt: k.RevokedAt}
}

func decodeName(r *http.Request) (string, error) {
	var req createRequest
	if err := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 1<<20)).Decode(&req); err != nil {
		return "", apperr.New(http.StatusBadRequest, "invalid_request", "invalid JSON body")
	}
	return req.Name, nil
}

// pathUUID parses a path parameter; a malformed ID is reported as the
// resource not being found.
func pathUUID(r *http.Request, name string, notFound *apperr.Error) (uuid.UUID, error) {
	id, err := uuid.Parse(r.PathValue(name))
	if err != nil {
		return uuid.Nil, notFound
	}
	return id, nil
}

func (c *controller) createGroup(w http.ResponseWriter, r *http.Request) {
	name, err := decodeName(r)
	if err != nil {
		apperr.Write(w, err)
		return
	}
	g, err := c.svc.createGroup(r.Context(), name)
	if err != nil {
		apperr.Write(w, err)
		return
	}
	apperr.WriteJSON(w, http.StatusCreated, toGroup(g))
}

func (c *controller) listGroups(w http.ResponseWriter, r *http.Request) {
	groups, err := c.svc.listGroups(r.Context())
	if err != nil {
		apperr.Write(w, err)
		return
	}
	out := make([]groupResponse, 0, len(groups))
	for _, g := range groups {
		out = append(out, toGroup(g))
	}
	apperr.WriteJSON(w, http.StatusOK, out)
}

func (c *controller) deleteGroup(w http.ResponseWriter, r *http.Request) {
	id, err := pathUUID(r, "groupId", errGroupNotFound)
	if err == nil {
		err = c.svc.deleteGroup(r.Context(), id)
	}
	if err != nil {
		apperr.Write(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (c *controller) createKey(w http.ResponseWriter, r *http.Request) {
	gid, err := pathUUID(r, "groupId", errGroupNotFound)
	if err != nil {
		apperr.Write(w, err)
		return
	}
	name, err := decodeName(r)
	if err != nil {
		apperr.Write(w, err)
		return
	}
	k, plaintext, err := c.svc.createKey(r.Context(), gid, name)
	if err != nil {
		apperr.Write(w, err)
		return
	}
	apperr.WriteJSON(w, http.StatusCreated, createdKeyResponse{keyResponse: toKey(k), Key: plaintext})
}

func (c *controller) listKeys(w http.ResponseWriter, r *http.Request) {
	gid, err := pathUUID(r, "groupId", errGroupNotFound)
	if err != nil {
		apperr.Write(w, err)
		return
	}
	keys, err := c.svc.listKeys(r.Context(), gid)
	if err != nil {
		apperr.Write(w, err)
		return
	}
	out := make([]keyResponse, 0, len(keys))
	for _, k := range keys {
		out = append(out, toKey(k))
	}
	apperr.WriteJSON(w, http.StatusOK, out)
}

func (c *controller) revokeKey(w http.ResponseWriter, r *http.Request) {
	gid, err := pathUUID(r, "groupId", errGroupNotFound)
	if err != nil {
		apperr.Write(w, err)
		return
	}
	kid, err := pathUUID(r, "keyId", errKeyNotFound)
	if err == nil {
		err = c.svc.revokeKey(r.Context(), gid, kid)
	}
	if err != nil {
		apperr.Write(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
