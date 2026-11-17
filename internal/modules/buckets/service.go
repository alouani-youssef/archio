package buckets

import (
	"context"
	"net/http"
	"regexp"

	"archio/internal/auth"
	"archio/internal/ent"
	apperr "archio/pkg/errors"
	"github.com/google/uuid"
)

type service struct{ repo *repository }

var (
	nameRE = regexp.MustCompile(`^[a-z0-9][a-z0-9.-]{1,61}[a-z0-9]$`)

	errUnauthorized = apperr.New(http.StatusUnauthorized, "unauthorized", "missing or invalid api key")
	errNotFound     = apperr.New(http.StatusNotFound, "bucket_not_found", "bucket not found")
	errNotEmpty     = apperr.New(http.StatusConflict, "bucket_not_empty", "bucket still contains objects")
)

// ValidateName checks a bucket name against the instance-wide naming rule.
func ValidateName(name string) error {
	if !nameRE.MatchString(name) {
		return apperr.New(http.StatusBadRequest, "invalid_bucket_name",
			"bucket name must be 3-63 characters of lowercase letters, digits, '.' or '-', starting and ending with a letter or digit")
	}
	return nil
}

// Resolve returns the named bucket of the group, or a 404 bucket_not_found
// when it does not exist or belongs to another group.
func Resolve(ctx context.Context, client *ent.Client, groupID uuid.UUID, name string) (*ent.Bucket, error) {
	b, err := (&repository{client: client}).find(ctx, groupID, name)
	if ent.IsNotFound(err) {
		return nil, errNotFound
	}
	return b, err
}

func groupOf(ctx context.Context) (uuid.UUID, error) {
	id, ok := auth.GroupFrom(ctx)
	if !ok {
		return uuid.Nil, errUnauthorized
	}
	return id, nil
}

func (s *service) create(ctx context.Context, name, description string) (*ent.Bucket, error) {
	gid, err := groupOf(ctx)
	if err != nil {
		return nil, err
	}
	if err := ValidateName(name); err != nil {
		return nil, err
	}
	b, err := s.repo.create(ctx, gid, name, description)
	if ent.IsConstraintError(err) {
		return nil, apperr.New(http.StatusConflict, "bucket_exists", "a bucket with this name already exists")
	}
	return b, err
}

func (s *service) list(ctx context.Context) ([]*ent.Bucket, error) {
	gid, err := groupOf(ctx)
	if err != nil {
		return nil, err
	}
	return s.repo.list(ctx, gid)
}

func (s *service) get(ctx context.Context, name string) (*ent.Bucket, error) {
	gid, err := groupOf(ctx)
	if err != nil {
		return nil, err
	}
	return Resolve(ctx, s.repo.client, gid, name)
}

func (s *service) delete(ctx context.Context, name string) error {
	gid, err := groupOf(ctx)
	if err != nil {
		return err
	}
	found, notEmpty, err := s.repo.delete(ctx, gid, name)
	switch {
	case ent.IsConstraintError(err):
		return errNotEmpty
	case err != nil:
		return err
	case !found:
		return errNotFound
	case notEmpty:
		return errNotEmpty
	}
	return nil
}
