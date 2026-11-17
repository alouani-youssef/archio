package buckets

import (
	"encoding/json"
	"net/http"

	"archio/internal/ent"
	apperr "archio/pkg/errors"
)

type controller struct{ svc *service }

func toBucket(b *ent.Bucket) bucketResponse {
	return bucketResponse{Name: b.Name, Description: b.Description, CreatedAt: b.CreatedAt}
}

func (c *controller) create(w http.ResponseWriter, r *http.Request) {
	var req createRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		apperr.Write(w, apperr.New(http.StatusBadRequest, "invalid_request", "invalid JSON body"))
		return
	}
	b, err := c.svc.create(r.Context(), req.Name, req.Description)
	if err != nil {
		apperr.Write(w, err)
		return
	}
	apperr.WriteJSON(w, http.StatusCreated, toBucket(b))
}

func (c *controller) list(w http.ResponseWriter, r *http.Request) {
	bs, err := c.svc.list(r.Context())
	if err != nil {
		apperr.Write(w, err)
		return
	}
	out := listResponse{Buckets: make([]bucketResponse, 0, len(bs))}
	for _, b := range bs {
		out.Buckets = append(out.Buckets, toBucket(b))
	}
	apperr.WriteJSON(w, http.StatusOK, out)
}

func (c *controller) get(w http.ResponseWriter, r *http.Request) {
	b, err := c.svc.get(r.Context(), r.PathValue("bucket"))
	if err != nil {
		apperr.Write(w, err)
		return
	}
	apperr.WriteJSON(w, http.StatusOK, toBucket(b))
}

func (c *controller) delete(w http.ResponseWriter, r *http.Request) {
	if err := c.svc.delete(r.Context(), r.PathValue("bucket")); err != nil {
		apperr.Write(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
