package admin

import (
	"context"
	"net/http"
	"strings"

	"archio/internal/auth"
	"archio/internal/ent"
	"archio/internal/middlewares"
	apperr "archio/pkg/errors"
	"github.com/google/uuid"
)

type service struct{ repo *repository }

var (
	errGroupNotFound = apperr.New(http.StatusNotFound, "group_not_found", "group not found")
	errKeyNotFound   = apperr.New(http.StatusNotFound, "key_not_found", "key not found")
	errUnauthorized  = apperr.New(http.StatusUnauthorized, "unauthorized", "invalid api key")
)

func validName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", apperr.New(http.StatusBadRequest, "invalid_request", "name is required")
	}
	return name, nil
}

func (s *service) createGroup(ctx context.Context, name string) (*ent.Group, error) {
	name, err := validName(name)
	if err != nil {
		return nil, err
	}
	g, err := s.repo.createGroup(ctx, name)
	if ent.IsConstraintError(err) {
		return nil, apperr.New(http.StatusConflict, "group_exists", "a group with this name already exists")
	}
	return g, err
}

func (s *service) listGroups(ctx context.Context) ([]*ent.Group, error) {
	return s.repo.listGroups(ctx)
}

func (s *service) deleteGroup(ctx context.Context, id uuid.UUID) error {
	found, hasBuckets, err := s.repo.deleteGroup(ctx, id)
	switch {
	case ent.IsConstraintError(err):
		// A bucket appeared between the check and the delete.
		return apperr.New(http.StatusConflict, "group_not_empty", "group still has buckets")
	case err != nil:
		return err
	case !found:
		return errGroupNotFound
	case hasBuckets:
		return apperr.New(http.StatusConflict, "group_not_empty", "group still has buckets")
	}
	return nil
}

func (s *service) requireGroup(ctx context.Context, id uuid.UUID) error {
	ok, err := s.repo.groupExists(ctx, id)
	if err != nil {
		return err
	}
	if !ok {
		return errGroupNotFound
	}
	return nil
}

// createKey returns the stored key and its plaintext, which is never persisted.
func (s *service) createKey(ctx context.Context, groupID uuid.UUID, name string) (*ent.ApiKey, string, error) {
	name, err := validName(name)
	if err != nil {
		return nil, "", err
	}
	if err := s.requireGroup(ctx, groupID); err != nil {
		return nil, "", err
	}
	plaintext, prefix, hash, err := auth.GenerateKey()
	if err != nil {
		return nil, "", err
	}
	k, err := s.repo.createKey(ctx, groupID, name, prefix, hash)
	if err != nil {
		return nil, "", err
	}
	return k, plaintext, nil
}

func (s *service) listKeys(ctx context.Context, groupID uuid.UUID) ([]*ent.ApiKey, error) {
	if err := s.requireGroup(ctx, groupID); err != nil {
		return nil, err
	}
	return s.repo.listKeys(ctx, groupID)
}

func (s *service) revokeKey(ctx context.Context, groupID, keyID uuid.UUID) error {
	if err := s.requireGroup(ctx, groupID); err != nil {
		return err
	}
	ok, err := s.repo.revokeKey(ctx, groupID, keyID)
	if err != nil {
		return err
	}
	if !ok {
		return errKeyNotFound
	}
	return nil
}

type keyAuthenticator struct{ repo *repository }

// NewKeyAuthenticator authenticates API keys against the database.
func NewKeyAuthenticator(client *ent.Client) middlewares.KeyAuthenticator {
	return &keyAuthenticator{repo: &repository{client: client}}
}

func (a *keyAuthenticator) Authenticate(ctx context.Context, keyHash string) (uuid.UUID, error) {
	k, err := a.repo.findActiveKey(ctx, keyHash)
	if ent.IsNotFound(err) {
		return uuid.Nil, errUnauthorized
	}
	if err != nil {
		return uuid.Nil, err
	}
	if err := a.repo.touchKey(ctx, k.ID); err != nil {
		return uuid.Nil, err
	}
	return k.Edges.Group.ID, nil
}
