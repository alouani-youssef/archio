package objects

import (
	"net/http"
	"time"

	"archio/internal/ent"
	apperr "archio/pkg/errors"
)

// uploadDeadline replaces the server-wide read/write timeouts for uploads.
const uploadDeadline = time.Hour

type controller struct {
	svc            *service
	publicBaseURL  string
	maxUploadBytes int64
}

func (c *controller) toVersion(key string, v *ent.ObjectVersion) versionResponse {
	id := v.ID.String()
	return versionResponse{
		VersionID:      id,
		Key:            key,
		Size:           v.Size,
		ChecksumSha256: v.ChecksumSha256,
		ContentType:    v.ContentType,
		CreatedAt:      v.CreatedAt,
		ReadURL:        c.publicBaseURL + "/v1/objects/" + id,
	}
}

func (c *controller) upload(w http.ResponseWriter, r *http.Request) {
	rc := http.NewResponseController(w)
	deadline := time.Now().Add(uploadDeadline)
	// Not every ResponseWriter supports deadlines (e.g. in tests); the
	// server-wide timeouts then remain in effect.
	_ = rc.SetReadDeadline(deadline)
	_ = rc.SetWriteDeadline(deadline)
	r.Body = http.MaxBytesReader(w, r.Body, c.maxUploadBytes)

	key := r.PathValue("key")
	v, err := c.svc.upload(r.Context(), r.PathValue("bucket"), key, r.Header.Get("Content-Type"), r.Body)
	if err != nil {
		apperr.Write(w, err)
		return
	}
	apperr.WriteJSON(w, http.StatusCreated, c.toVersion(key, v))
}

func (c *controller) list(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	objs, next, err := c.svc.list(r.Context(), r.PathValue("bucket"), q.Get("prefix"), q.Get("cursor"), q.Get("limit"))
	if err != nil {
		apperr.Write(w, err)
		return
	}
	out := listResponse{Objects: make([]objectResponse, 0, len(objs)), NextCursor: next}
	for _, o := range objs {
		item := objectResponse{Key: o.Identifier, UpdatedAt: o.UpdatedAt}
		if cv := o.Edges.CurrentVersion; cv != nil {
			v := c.toVersion(o.Identifier, cv)
			item.CurrentVersion = &v
		}
		out.Objects = append(out.Objects, item)
	}
	apperr.WriteJSON(w, http.StatusOK, out)
}

func (c *controller) versions(w http.ResponseWriter, r *http.Request) {
	o, err := c.svc.versions(r.Context(), r.PathValue("bucket"), r.PathValue("key"))
	if err != nil {
		apperr.Write(w, err)
		return
	}
	out := versionsResponse{Versions: make([]versionResponse, 0, len(o.Edges.Versions))}
	for _, v := range o.Edges.Versions {
		out.Versions = append(out.Versions, c.toVersion(o.Identifier, v))
	}
	apperr.WriteJSON(w, http.StatusOK, out)
}

func (c *controller) delete(w http.ResponseWriter, r *http.Request) {
	if err := c.svc.delete(r.Context(), r.PathValue("bucket"), r.PathValue("key")); err != nil {
		apperr.Write(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
