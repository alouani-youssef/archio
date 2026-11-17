package buckets

import (
	"context"

	"archio/internal/ent"
	"archio/internal/ent/bucket"
	"archio/internal/ent/group"
	"archio/internal/ent/object"
	"github.com/google/uuid"
)

type repository struct{ client *ent.Client }

func (r *repository) create(ctx context.Context, groupID uuid.UUID, name, description string) (*ent.Bucket, error) {
	c := r.client.Bucket.Create().SetName(name).SetGroupID(groupID)
	if description != "" {
		c.SetDescription(description)
	}
	return c.Save(ctx)
}

func (r *repository) list(ctx context.Context, groupID uuid.UUID) ([]*ent.Bucket, error) {
	return r.client.Bucket.Query().
		Where(bucket.HasGroupWith(group.ID(groupID))).
		Order(ent.Asc(bucket.FieldName)).All(ctx)
}

func (r *repository) find(ctx context.Context, groupID uuid.UUID, name string) (*ent.Bucket, error) {
	return r.client.Bucket.Query().
		Where(bucket.Name(name), bucket.HasGroupWith(group.ID(groupID))).Only(ctx)
}

// delete removes an empty bucket of the group. found=false means no such
// bucket; notEmpty=true means it still has objects and nothing was deleted.
func (r *repository) delete(ctx context.Context, groupID uuid.UUID, name string) (found, notEmpty bool, err error) {
	tx, err := r.client.Tx(ctx)
	if err != nil {
		return false, false, err
	}
	defer func() {
		if err != nil || !found || notEmpty {
			_ = tx.Rollback()
		}
	}()
	b, err := tx.Bucket.Query().
		Where(bucket.Name(name), bucket.HasGroupWith(group.ID(groupID))).Only(ctx)
	if ent.IsNotFound(err) {
		return false, false, nil
	}
	if err != nil {
		return false, false, err
	}
	if notEmpty, err = tx.Object.Query().Where(object.HasBucketWith(bucket.ID(b.ID))).Exist(ctx); err != nil || notEmpty {
		return true, notEmpty, err
	}
	if err = tx.Bucket.DeleteOne(b).Exec(ctx); err != nil {
		return true, false, err
	}
	return true, false, tx.Commit()
}
