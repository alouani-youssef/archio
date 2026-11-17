package objects

import (
	"errors"
	"net/http"
	"time"

	"archio/internal/ent"
	"archio/internal/ent/bucket"
	"archio/internal/ent/group"
	"archio/internal/ent/object"
	"archio/internal/ent/objectversion"
	"archio/internal/storage"
	apperr "archio/pkg/errors"
	"github.com/google/uuid"
)

var errVersionNotFound = apperr.New(http.StatusNotFound, "version_not_found", "version not found")

// read serves the bytes of one version. A version outside the caller's group
// is reported as not found, never as forbidden.
func (c *controller) read(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	gid, err := groupOf(ctx)
	if err != nil {
		apperr.Write(w, err)
		return
	}
	id, err := uuid.Parse(r.PathValue("versionId"))
	if err != nil {
		apperr.Write(w, errVersionNotFound)
		return
	}
	v, err := c.svc.repo.client.ObjectVersion.Query().
		Where(
			objectversion.ID(id),
			objectversion.HasObjectWith(object.HasBucketWith(bucket.HasGroupWith(group.ID(gid)))),
		).Only(ctx)
	if ent.IsNotFound(err) {
		apperr.Write(w, errVersionNotFound)
		return
	}
	if err != nil {
		apperr.Write(w, err)
		return
	}
	file, err := c.svc.storage.Open(ctx, v.StorageKey)
	if errors.Is(err, storage.ErrNotFound) {
		// The blob is gone: either the object was just deleted (404) or the
		// file was lost (opaque 500).
		if exists, qerr := c.svc.repo.client.ObjectVersion.Query().Where(objectversion.ID(id)).Exist(ctx); qerr == nil && !exists {
			apperr.Write(w, errVersionNotFound)
			return
		}
	}
	if err != nil {
		apperr.Write(w, err)
		return
	}
	defer file.Close()

	// Not every ResponseWriter supports deadlines; ignore that error.
	_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(uploadDeadline))
	h := w.Header()
	h.Set("Content-Type", v.ContentType)
	h.Set("ETag", `"`+v.ChecksumSha256+`"`)
	h.Set("X-Checksum-Sha256", v.ChecksumSha256)
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Cache-Control", "private, max-age=31536000, immutable")
	http.ServeContent(w, r, "", v.CreatedAt, file)
}
