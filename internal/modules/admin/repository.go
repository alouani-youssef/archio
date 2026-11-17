package admin

import (
	"context"
	"time"

	"archio/internal/ent"
	"archio/internal/ent/apikey"
	"archio/internal/ent/bucket"
	"archio/internal/ent/group"
	"github.com/google/uuid"
)

type repository struct{ client *ent.Client }

func (r *repository) createGroup(ctx context.Context, name string) (*ent.Group, error) {
	return r.client.Group.Create().SetName(name).Save(ctx)
}

func (r *repository) listGroups(ctx context.Context) ([]*ent.Group, error) {
	return r.client.Group.Query().Order(ent.Asc(group.FieldCreatedAt)).All(ctx)
}

func (r *repository) groupExists(ctx context.Context, id uuid.UUID) (bool, error) {
	return r.client.Group.Query().Where(group.ID(id)).Exist(ctx)
}

// deleteGroup removes the group and its keys. It returns hasBuckets=true and
// deletes nothing when the group still owns buckets.
func (r *repository) deleteGroup(ctx context.Context, id uuid.UUID) (found, hasBuckets bool, err error) {
	tx, err := r.client.Tx(ctx)
	if err != nil {
		return false, false, err
	}
	defer func() {
		if err != nil || !found || hasBuckets {
			_ = tx.Rollback()
		}
	}()
	if found, err = tx.Group.Query().Where(group.ID(id)).Exist(ctx); err != nil || !found {
		return false, false, err
	}
	if hasBuckets, err = tx.Bucket.Query().Where(bucket.HasGroupWith(group.ID(id))).Exist(ctx); err != nil || hasBuckets {
		return true, hasBuckets, err
	}
	if _, err = tx.ApiKey.Delete().Where(apikey.HasGroupWith(group.ID(id))).Exec(ctx); err != nil {
		return true, false, err
	}
	if err = tx.Group.DeleteOneID(id).Exec(ctx); err != nil {
		return true, false, err
	}
	return true, false, tx.Commit()
}

func (r *repository) createKey(ctx context.Context, groupID uuid.UUID, name, prefix, hash string) (*ent.ApiKey, error) {
	return r.client.ApiKey.Create().SetGroupID(groupID).SetName(name).SetPrefix(prefix).SetKeyHash(hash).Save(ctx)
}

func (r *repository) listKeys(ctx context.Context, groupID uuid.UUID) ([]*ent.ApiKey, error) {
	return r.client.ApiKey.Query().
		Where(apikey.HasGroupWith(group.ID(groupID))).
		Order(ent.Asc(apikey.FieldCreatedAt)).All(ctx)
}

// revokeKey sets revoked_at on a key of the group, keeping an earlier
// revocation time. It reports whether the key exists in that group.
func (r *repository) revokeKey(ctx context.Context, groupID, keyID uuid.UUID) (bool, error) {
	owned := apikey.And(apikey.ID(keyID), apikey.HasGroupWith(group.ID(groupID)))
	if _, err := r.client.ApiKey.Update().
		Where(owned, apikey.RevokedAtIsNil()).
		SetRevokedAt(time.Now()).Save(ctx); err != nil {
		return false, err
	}
	return r.client.ApiKey.Query().Where(owned).Exist(ctx)
}

// findActiveKey returns the non-revoked key with the given hash.
func (r *repository) findActiveKey(ctx context.Context, hash string) (*ent.ApiKey, error) {
	return r.client.ApiKey.Query().
		Where(apikey.KeyHash(hash), apikey.RevokedAtIsNil()).
		WithGroup().Only(ctx)
}

// touchKey bumps last_used_at at most once a minute with a single UPDATE.
func (r *repository) touchKey(ctx context.Context, id uuid.UUID) error {
	now := time.Now()
	_, err := r.client.ApiKey.Update().
		Where(apikey.ID(id), apikey.Or(apikey.LastUsedAtIsNil(), apikey.LastUsedAtLT(now.Add(-time.Minute)))).
		SetLastUsedAt(now).Save(ctx)
	return err
}
