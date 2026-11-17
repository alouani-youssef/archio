package database_test

import (
	"context"
	"testing"

	"archio/internal/ent"
	"archio/internal/testutil"
)

func TestSchema_GroupBucketObjectVersion(t *testing.T) {
	client := testutil.OpenDB(t)
	ctx := context.Background()

	group := client.Group.Create().SetName(testutil.Unique("group")).SaveX(ctx)
	other := client.Group.Create().SetName(testutil.Unique("group")).SaveX(ctx)
	bucketName := testutil.Unique("bucket")
	bucket := client.Bucket.Create().SetName(bucketName).SetGroup(group).SaveX(ctx)
	object := client.Object.Create().SetIdentifier("obj").SetBucket(bucket).SaveX(ctx)

	newVersion := func(key string) *ent.ObjectVersion {
		return client.ObjectVersion.Create().
			SetSize(3).
			SetChecksumSha256("abc").
			SetContentType("text/plain").
			SetStorageKey(key).
			SetObject(object).
			SaveX(ctx)
	}
	newVersion("k1")
	second := newVersion("k2")

	object.Update().SetCurrentVersion(second).ExecX(ctx)

	if n := object.QueryVersions().CountX(ctx); n != 2 {
		t.Fatalf("versions = %d, want 2", n)
	}
	if id := object.QueryCurrentVersion().OnlyIDX(ctx); id != second.ID {
		t.Fatalf("current version = %v, want %v", id, second.ID)
	}

	_, err := client.Bucket.Create().SetName(bucketName).SetGroup(other).Save(ctx)
	if !ent.IsConstraintError(err) {
		t.Fatalf("duplicate bucket name err = %v, want constraint error", err)
	}
}
