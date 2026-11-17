// Package storage defines the blob storage interface and its backends.
package storage

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/google/uuid"
)

// ErrNotFound is returned when a blob does not exist.
var ErrNotFound = errors.New("storage: blob not found")

// Storage stores immutable blobs addressed by a storage key.
type Storage interface {
	// Put streams r into storage and returns the byte count and SHA-256 hex digest.
	// Errors from r are returned such that errors.As still finds them.
	Put(ctx context.Context, storageKey string, r io.Reader) (size int64, sha256Hex string, err error)
	Open(ctx context.Context, storageKey string) (io.ReadSeekCloser, error)
	// Delete removes a blob; deleting a missing blob is not an error.
	Delete(ctx context.Context, storageKey string) error
}

// Key returns the storage key "<group>/<bucket>/<version>".
func Key(groupID uuid.UUID, bucketID int, versionID uuid.UUID) string {
	return fmt.Sprintf("%s/%d/%s", groupID, bucketID, versionID)
}
