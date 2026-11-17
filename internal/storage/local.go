package storage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

const copyChunk = 256 * 1024

// Local stores blobs on the local filesystem under a root directory.
type Local struct {
	root string
	tmp  string
}

// NewLocal creates root and root/.tmp and verifies they are writable.
func NewLocal(root string) (*Local, error) {
	tmp := filepath.Join(root, ".tmp")
	if err := os.MkdirAll(tmp, 0o750); err != nil {
		return nil, fmt.Errorf("storage: create root: %w", err)
	}
	probe, err := os.CreateTemp(tmp, "probe-*")
	if err != nil {
		return nil, fmt.Errorf("storage: root not writable: %w", err)
	}
	name := probe.Name()
	probe.Close()
	if err := os.Remove(name); err != nil {
		return nil, fmt.Errorf("storage: root not writable: %w", err)
	}
	return &Local{root: root, tmp: tmp}, nil
}

func (l *Local) path(key string) (string, error) {
	if key == "" || filepath.IsAbs(key) || strings.HasPrefix(key, "/") {
		return "", fmt.Errorf("storage: invalid key %q", key)
	}
	for _, seg := range strings.Split(filepath.ToSlash(key), "/") {
		if seg == ".." {
			return "", fmt.Errorf("storage: invalid key %q", key)
		}
	}
	p := filepath.Join(l.root, key)
	rel, err := filepath.Rel(l.root, p)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("storage: invalid key %q", key)
	}
	return p, nil
}

type ctxReader struct {
	ctx context.Context
	r   io.Reader
}

func (c ctxReader) Read(p []byte) (int, error) {
	if err := c.ctx.Err(); err != nil {
		return 0, err
	}
	if len(p) > copyChunk {
		p = p[:copyChunk]
	}
	return c.r.Read(p)
}

// Put writes r to a temp file, then atomically renames it into place.
func (l *Local) Put(ctx context.Context, storageKey string, r io.Reader) (size int64, sha256Hex string, err error) {
	dst, err := l.path(storageKey)
	if err != nil {
		return 0, "", err
	}
	if err := ctx.Err(); err != nil {
		return 0, "", err
	}
	f, err := os.CreateTemp(l.tmp, "put-*")
	if err != nil {
		return 0, "", fmt.Errorf("storage: create temp: %w", err)
	}
	tmpName := f.Name()
	defer func() {
		if err != nil {
			f.Close()
			os.Remove(tmpName)
		}
	}()

	h := sha256.New()
	size, err = io.Copy(io.MultiWriter(f, h), ctxReader{ctx: ctx, r: r})
	if err != nil {
		return 0, "", err
	}
	if err = f.Sync(); err != nil {
		return 0, "", fmt.Errorf("storage: sync: %w", err)
	}
	if err = f.Close(); err != nil {
		return 0, "", fmt.Errorf("storage: close: %w", err)
	}
	dir := filepath.Dir(dst)
	// Record which directories MkdirAll will create so their parents can be synced.
	var created []string
	for d := dir; d != l.root && d != filepath.Dir(d); d = filepath.Dir(d) {
		if _, serr := os.Stat(d); serr == nil {
			break
		}
		created = append(created, d)
	}
	if err = os.MkdirAll(dir, 0o750); err != nil {
		return 0, "", fmt.Errorf("storage: mkdir: %w", err)
	}
	if err = os.Rename(tmpName, dst); err != nil {
		return 0, "", fmt.Errorf("storage: rename: %w", err)
	}
	// Make the rename and any newly created directory entries durable.
	toSync := []string{dir}
	for _, c := range created {
		toSync = append(toSync, filepath.Dir(c))
	}
	seen := map[string]bool{}
	for _, d := range toSync {
		if seen[d] {
			continue
		}
		seen[d] = true
		if err = syncDir(d); err != nil {
			return 0, "", err
		}
	}
	return size, hex.EncodeToString(h.Sum(nil)), nil
}

// Open returns the blob, or ErrNotFound.
func (l *Local) Open(_ context.Context, storageKey string) (io.ReadSeekCloser, error) {
	p, err := l.path(storageKey)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(p)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("storage: open: %w", err)
	}
	return f, nil
}

// Delete removes the blob; a missing blob is not an error.
func (l *Local) Delete(_ context.Context, storageKey string) error {
	p, err := l.path(storageKey)
	if err != nil {
		return err
	}
	if err := os.Remove(p); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("storage: delete: %w", err)
	}
	return nil
}

func syncDir(path string) error {
	d, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("storage: open dir for sync: %w", err)
	}
	if err := d.Sync(); err != nil {
		d.Close()
		return fmt.Errorf("storage: sync dir: %w", err)
	}
	if err := d.Close(); err != nil {
		return fmt.Errorf("storage: close dir: %w", err)
	}
	return nil
}
