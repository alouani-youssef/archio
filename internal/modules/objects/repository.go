package objects

import (
	"context"

	"archio/internal/ent"
	"archio/internal/ent/bucket"
	"archio/internal/ent/chunk"
	"archio/internal/ent/exactsearchvalue"
	"archio/internal/ent/metadata"
	"archio/internal/ent/object"
	"archio/internal/ent/objectversion"
	"archio/internal/ent/predicate"
	"github.com/google/uuid"
)

type repository struct{ client *ent.Client }

type newVersion struct {
	id          uuid.UUID
	size        int64
	checksum    string
	contentType string
	storageKey  string
}

func inBucket(bucketID int, key string) []predicate.Object {
	return []predicate.Object{object.Identifier(key), object.HasBucketWith(bucket.ID(bucketID))}
}

// createVersion gets or creates the object, adds the version and makes it
// current, all in one transaction. A constraint error means a concurrent
// upload created the object first; the caller may retry.
func (r *repository) createVersion(ctx context.Context, bucketID int, key string, nv newVersion) (v *ent.ObjectVersion, err error) {
	tx, err := r.client.Tx(ctx)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	o, err := tx.Object.Query().Where(inBucket(bucketID, key)...).Only(ctx)
	switch {
	case ent.IsNotFound(err):
		o, err = tx.Object.Create().SetIdentifier(key).SetBucketID(bucketID).Save(ctx)
	case err == nil:
		// Lock the row before the version's created_at is taken, so the version
		// committed last is both current and newest.
		o, err = tx.Object.UpdateOne(o).Save(ctx)
	}
	if err != nil {
		return nil, err
	}
	v, err = tx.ObjectVersion.Create().
		SetID(nv.id).
		SetSize(nv.size).
		SetChecksumSha256(nv.checksum).
		SetContentType(nv.contentType).
		SetStorageKey(nv.storageKey).
		SetObject(o).
		Save(ctx)
	if err != nil {
		return nil, err
	}
	if err = tx.Object.UpdateOne(o).SetCurrentVersion(v).Exec(ctx); err != nil {
		return nil, err
	}
	return v, tx.Commit()
}

// list returns up to limit+1 objects of the bucket after the cursor key,
// ordered by key, with their current versions loaded.
func (r *repository) list(ctx context.Context, bucketID int, prefix, after string, limit int) ([]*ent.Object, error) {
	q := r.client.Object.Query().Where(object.HasBucketWith(bucket.ID(bucketID)))
	if prefix != "" {
		q.Where(object.IdentifierHasPrefix(prefix))
	}
	if after != "" {
		q.Where(object.IdentifierGT(after))
	}
	return q.WithCurrentVersion().Order(ent.Asc(object.FieldIdentifier)).Limit(limit + 1).All(ctx)
}

// versions returns the object with its versions loaded, newest first.
func (r *repository) versions(ctx context.Context, bucketID int, key string) (*ent.Object, error) {
	return r.client.Object.Query().Where(inBucket(bucketID, key)...).
		WithVersions(func(q *ent.ObjectVersionQuery) {
			q.Order(ent.Desc(objectversion.FieldCreatedAt), ent.Asc(objectversion.FieldID))
		}).Only(ctx)
}

// delete removes the object with its versions and dependent rows in one
// transaction and returns the storage keys of the deleted versions.
func (r *repository) delete(ctx context.Context, bucketID int, key string) (storageKeys []string, err error) {
	tx, err := r.client.Tx(ctx)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	o, err := tx.Object.Query().Where(inBucket(bucketID, key)...).Only(ctx)
	if err != nil {
		return nil, err
	}
	if err = tx.Object.UpdateOne(o).ClearCurrentVersion().Exec(ctx); err != nil {
		return nil, err
	}
	ofObject := object.ID(o.ID)
	vs, err := tx.ObjectVersion.Query().Where(objectversion.HasObjectWith(ofObject)).All(ctx)
	if err != nil {
		return nil, err
	}
	if _, err = tx.ObjectVersion.Delete().Where(objectversion.HasObjectWith(ofObject)).Exec(ctx); err != nil {
		return nil, err
	}
	if _, err = tx.MetaData.Delete().Where(metadata.HasObjectWith(ofObject)).Exec(ctx); err != nil {
		return nil, err
	}
	if _, err = tx.Chunk.Delete().Where(chunk.HasObjectWith(ofObject)).Exec(ctx); err != nil {
		return nil, err
	}
	if _, err = tx.ExactSearchValue.Delete().Where(exactsearchvalue.HasObjectWith(ofObject)).Exec(ctx); err != nil {
		return nil, err
	}
	if err = tx.Object.DeleteOne(o).Exec(ctx); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	for _, v := range vs {
		storageKeys = append(storageKeys, v.StorageKey)
	}
	return storageKeys, nil
}
