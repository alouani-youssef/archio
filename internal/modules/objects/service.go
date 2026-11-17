package objects

import (
	"context"
	"encoding/base64"
	"errors"
	"io"
	"log"
	"net/http"
	"strconv"
	"time"

	"archio/internal/auth"
	"archio/internal/ent"
	"archio/internal/events"
	"archio/internal/modules/buckets"
	"archio/internal/storage"
	apperr "archio/pkg/errors"
	"github.com/google/uuid"
)

const (
	defaultContentType = "application/octet-stream"
	defaultListLimit   = 100
	maxListLimit       = 1000
	publishTimeout     = 2 * time.Second
	// dbTimeout bounds the upload's DB transaction, which runs detached from
	// the request: once the body is stored, a client disconnect must not
	// abort (or make ambiguous) the commit.
	dbTimeout = 30 * time.Second
)

var (
	errUnauthorized  = apperr.New(http.StatusUnauthorized, "unauthorized", "missing or invalid api key")
	errNotFound      = apperr.New(http.StatusNotFound, "object_not_found", "object not found")
	errTooLarge      = apperr.New(http.StatusRequestEntityTooLarge, "payload_too_large", "upload exceeds the maximum allowed size")
	errBodyRead      = apperr.New(http.StatusBadRequest, "invalid_request", "failed to read the request body")
	errInvalidLimit  = apperr.New(http.StatusBadRequest, "invalid_limit", "limit must be between 1 and 1000")
	errInvalidCursor = apperr.New(http.StatusBadRequest, "invalid_cursor", "invalid cursor")
)

type service struct {
	repo      *repository
	storage   storage.Storage
	publisher events.Publisher
}

func groupOf(ctx context.Context) (uuid.UUID, error) {
	id, ok := auth.GroupFrom(ctx)
	if !ok {
		return uuid.Nil, errUnauthorized
	}
	return id, nil
}

func (s *service) bucket(ctx context.Context, name string) (uuid.UUID, *ent.Bucket, error) {
	gid, err := groupOf(ctx)
	if err != nil {
		return uuid.Nil, nil, err
	}
	b, err := buckets.Resolve(ctx, s.repo.client, gid, name)
	return gid, b, err
}

// trackingReader remembers the error of the underlying request body, so a
// client-side read failure can be told apart from a storage failure.
type trackingReader struct {
	r   io.Reader
	err error
}

func (t *trackingReader) Read(p []byte) (int, error) {
	n, err := t.r.Read(p)
	if err != nil && err != io.EOF {
		t.err = err
	}
	return n, err
}

// upload stores body as a new version of the object and makes it current.
func (s *service) upload(ctx context.Context, bucketName, key, contentType string, body io.Reader) (*ent.ObjectVersion, error) {
	gid, b, err := s.bucket(ctx, bucketName)
	if err != nil {
		return nil, err
	}
	if err := ValidateKey(key); err != nil {
		return nil, err
	}
	if contentType == "" {
		contentType = defaultContentType
	}

	versionID := uuid.New()
	storageKey := storage.Key(gid, b.ID, versionID)
	tr := &trackingReader{r: body}
	size, sum, err := s.storage.Put(ctx, storageKey, tr)
	if err != nil {
		var tooLarge *http.MaxBytesError
		switch {
		case errors.As(err, &tooLarge):
			return nil, errTooLarge
		case tr.err != nil || ctx.Err() != nil:
			return nil, errBodyRead
		}
		return nil, err
	}

	nv := newVersion{id: versionID, size: size, checksum: sum, contentType: contentType, storageKey: storageKey}
	dbCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), dbTimeout)
	defer cancel()
	v, err := s.repo.createVersion(dbCtx, b.ID, key, nv)
	var commitErr *commitError
	if ent.IsConstraintError(err) && !errors.As(err, &commitErr) {
		// A concurrent upload created the object first; it exists now.
		v, err = s.repo.createVersion(dbCtx, b.ID, key, nv)
	}
	if errors.As(err, &commitErr) {
		// The row may exist: keep the blob (an orphan is harmless, a row
		// without bytes is not).
		log.Printf("objects: commit outcome unknown for version %s, keeping blob %s: %v", versionID, storageKey, err)
		return nil, err
	}
	if err != nil {
		if derr := s.storage.Delete(context.WithoutCancel(ctx), storageKey); derr != nil {
			log.Printf("objects: delete orphaned blob %s: %v", storageKey, derr)
		}
		return nil, err
	}

	s.publishCreated(ctx, gid, b, key, v)
	return v, nil
}

func (s *service) publishCreated(ctx context.Context, gid uuid.UUID, b *ent.Bucket, key string, v *ent.ObjectVersion) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), publishTimeout)
	defer cancel()
	err := s.publisher.Publish(ctx, events.ObjectVersionCreated, events.ObjectVersionCreatedEvent{
		VersionID:      v.ID,
		ObjectKey:      key,
		BucketID:       b.ID,
		BucketName:     b.Name,
		GroupID:        gid,
		Size:           v.Size,
		ChecksumSHA256: v.ChecksumSha256,
		ContentType:    v.ContentType,
		CreatedAt:      v.CreatedAt,
	})
	if err != nil {
		log.Printf("objects: publish %s for version %s: %v", events.ObjectVersionCreated, v.ID, err)
	}
}

// list returns one page of the bucket's objects ordered by key, and the
// cursor of the next page ("" when this is the last page).
func (s *service) list(ctx context.Context, bucketName, prefix, cursor, limitParam string) ([]*ent.Object, string, error) {
	_, b, err := s.bucket(ctx, bucketName)
	if err != nil {
		return nil, "", err
	}
	limit, err := parseLimit(limitParam)
	if err != nil {
		return nil, "", err
	}
	after, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return nil, "", errInvalidCursor
	}
	objs, err := s.repo.list(ctx, b.ID, prefix, string(after), limit)
	if err != nil {
		return nil, "", err
	}
	next := ""
	if len(objs) > limit {
		objs = objs[:limit]
		next = base64.RawURLEncoding.EncodeToString([]byte(objs[limit-1].Identifier))
	}
	return objs, next, nil
}

func parseLimit(s string) (int, error) {
	if s == "" {
		return defaultListLimit, nil
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 1 || n > maxListLimit {
		return 0, errInvalidLimit
	}
	return n, nil
}

// versions returns the object with its versions, newest first.
func (s *service) versions(ctx context.Context, bucketName, key string) (*ent.Object, error) {
	_, b, err := s.bucket(ctx, bucketName)
	if err != nil {
		return nil, err
	}
	o, err := s.repo.versions(ctx, b.ID, key)
	if ent.IsNotFound(err) {
		return nil, errNotFound
	}
	return o, err
}

// delete removes the object and all its versions, then their blobs. A blob
// that fails to delete is logged and left behind as harmless garbage.
func (s *service) delete(ctx context.Context, bucketName, key string) error {
	_, b, err := s.bucket(ctx, bucketName)
	if err != nil {
		return err
	}
	storageKeys, err := s.repo.delete(ctx, b.ID, key)
	if ent.IsNotFound(err) {
		return errNotFound
	}
	if err != nil {
		return err
	}
	for _, k := range storageKeys {
		if err := s.storage.Delete(context.WithoutCancel(ctx), k); err != nil {
			log.Printf("objects: delete blob %s: %v", k, err)
		}
	}
	return nil
}
