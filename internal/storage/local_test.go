package storage

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	helloSHA = "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824"
	emptySHA = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
)

func newTestLocal(t *testing.T) (*Local, string) {
	t.Helper()
	root := t.TempDir()
	l, err := NewLocal(root)
	if err != nil {
		t.Fatalf("NewLocal: %v", err)
	}
	return l, root
}

func assertTmpEmpty(t *testing.T, root string) {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(root, ".tmp"))
	if err != nil {
		t.Fatalf("read .tmp: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf(".tmp has %d entries, want 0", len(entries))
	}
}

type errReader struct {
	data []byte
	err  error
}

func (e *errReader) Read(p []byte) (int, error) {
	if len(e.data) == 0 {
		return 0, e.err
	}
	n := copy(p, e.data)
	e.data = e.data[n:]
	return n, nil
}

func TestLocal_PutOpenRoundTrip(t *testing.T) {
	l, _ := newTestLocal(t)
	ctx := context.Background()
	size, sum, err := l.Put(ctx, "g/1/v", strings.NewReader("hello"))
	if err != nil {
		t.Fatal(err)
	}
	if size != 5 || sum != helloSHA {
		t.Fatalf("got %d %s", size, sum)
	}
	f, err := l.Open(ctx, "g/1/v")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	b, _ := io.ReadAll(f)
	if string(b) != "hello" {
		t.Fatalf("got %q", b)
	}
}

func TestLocal_PutEmpty(t *testing.T) {
	l, _ := newTestLocal(t)
	size, sum, err := l.Put(context.Background(), "g/1/v", strings.NewReader(""))
	if err != nil {
		t.Fatal(err)
	}
	if size != 0 || sum != emptySHA {
		t.Fatalf("got %d %s", size, sum)
	}
}

func TestLocal_PutReaderErrorLeavesNothing(t *testing.T) {
	l, root := newTestLocal(t)
	ctx := context.Background()
	_, _, err := l.Put(ctx, "g/1/v", &errReader{data: []byte("abc"), err: io.ErrUnexpectedEOF})
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("err = %v", err)
	}
	if _, err := l.Open(ctx, "g/1/v"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Open err = %v", err)
	}
	assertTmpEmpty(t, root)
}

func TestLocal_PutCanceledContext(t *testing.T) {
	l, root := newTestLocal(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, err := l.Put(ctx, "g/1/v", strings.NewReader("hello"))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
	assertTmpEmpty(t, root)
}

func TestLocal_OpenMissing(t *testing.T) {
	l, _ := newTestLocal(t)
	if _, err := l.Open(context.Background(), "g/1/missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v", err)
	}
}

func TestLocal_DeleteMissing(t *testing.T) {
	l, _ := newTestLocal(t)
	if err := l.Delete(context.Background(), "g/1/missing"); err != nil {
		t.Fatal(err)
	}
}

func TestLocal_DeleteExisting(t *testing.T) {
	l, _ := newTestLocal(t)
	ctx := context.Background()
	if _, _, err := l.Put(ctx, "g/1/v", strings.NewReader("x")); err != nil {
		t.Fatal(err)
	}
	if err := l.Delete(ctx, "g/1/v"); err != nil {
		t.Fatal(err)
	}
	if _, err := l.Open(ctx, "g/1/v"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v", err)
	}
}

func TestLocal_RejectsEscapingKey(t *testing.T) {
	l, _ := newTestLocal(t)
	ctx := context.Background()
	for _, k := range []string{"../x", "/abs", "a/../../x", ""} {
		if _, _, err := l.Put(ctx, k, strings.NewReader("x")); err == nil {
			t.Errorf("Put(%q) succeeded", k)
		}
		if _, err := l.Open(ctx, k); err == nil || errors.Is(err, ErrNotFound) {
			t.Errorf("Open(%q) err = %v", k, err)
		}
		if err := l.Delete(ctx, k); err == nil {
			t.Errorf("Delete(%q) succeeded", k)
		}
	}
}

func TestNewLocal_UnwritableRoot(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	parent := filepath.Join(t.TempDir(), "ro")
	if err := os.Mkdir(parent, 0o500); err != nil {
		t.Fatal(err)
	}
	if _, err := NewLocal(filepath.Join(parent, "root")); err == nil {
		t.Fatal("expected error")
	}
}
