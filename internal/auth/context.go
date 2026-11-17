package auth

import (
	"context"

	"github.com/google/uuid"
)

type groupKey struct{}

// WithGroup returns a context carrying the authenticated group ID.
func WithGroup(ctx context.Context, groupID uuid.UUID) context.Context {
	return context.WithValue(ctx, groupKey{}, groupID)
}

// GroupFrom returns the authenticated group ID, if any.
func GroupFrom(ctx context.Context) (uuid.UUID, bool) {
	id, ok := ctx.Value(groupKey{}).(uuid.UUID)
	return id, ok
}
