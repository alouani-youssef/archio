package objects

import (
	"bytes"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func (e *env) read(method, versionID string, header http.Header) *httptest.ResponseRecorder {
	return e.request(e.a, method, "/v1/objects/"+versionID, nil, header)
}

func assertReadHeaders(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	want := map[string]string{
		"Content-Type":      "text/plain",
		"Content-Length":    "5",
		"Etag":              `"` + helloSHA + `"`,
		"X-Checksum-Sha256": helloSHA,
		"Cache-Control":     "private, max-age=31536000, immutable",
	}
	for k, v := range want {
		if got := rec.Header().Get(k); got != v {
			t.Errorf("%s = %q, want %q", k, got, v)
		}
	}
}

func TestRead_ReturnsBytesAndHeaders(t *testing.T) {
	e := setup(t)
	v := mustPut(t, e, "hello.txt", "hello", "text/plain")
	rec := e.read("GET", v.VersionID, nil)
	if rec.Code != 200 || rec.Body.String() != "hello" {
		t.Fatalf("got %d %q", rec.Code, rec.Body)
	}
	assertReadHeaders(t, rec)
}

func TestRead_OldVersionStillServed(t *testing.T) {
	e := setup(t)
	v1 := mustPut(t, e, "hello.txt", "hello", "text/plain")
	mustPut(t, e, "hello.txt", "world", "text/plain")
	rec := e.read("GET", v1.VersionID, nil)
	if rec.Code != 200 || rec.Body.String() != "hello" {
		t.Fatalf("got %d %q", rec.Code, rec.Body)
	}
}

func TestRead_Head(t *testing.T) {
	e := setup(t)
	v := mustPut(t, e, "hello.txt", "hello", "text/plain")
	rec := e.read("HEAD", v.VersionID, nil)
	if rec.Code != 200 || rec.Body.Len() != 0 {
		t.Fatalf("got %d %q", rec.Code, rec.Body)
	}
	assertReadHeaders(t, rec)
}

func TestRead_IfNoneMatch(t *testing.T) {
	e := setup(t)
	v := mustPut(t, e, "hello.txt", "hello", "text/plain")
	rec := e.read("GET", v.VersionID, http.Header{"If-None-Match": {`"` + helloSHA + `"`}})
	if rec.Code != http.StatusNotModified || rec.Body.Len() != 0 {
		t.Fatalf("got %d %q", rec.Code, rec.Body)
	}
}

func TestRead_Range(t *testing.T) {
	e := setup(t)
	v := mustPut(t, e, "hello.txt", "hello", "text/plain")
	rec := e.read("GET", v.VersionID, http.Header{"Range": {"bytes=1-3"}})
	if rec.Code != http.StatusPartialContent || rec.Body.String() != "ell" {
		t.Fatalf("got %d %q", rec.Code, rec.Body)
	}
}

func TestRead_OtherGroupIs404(t *testing.T) {
	e := setup(t)
	v := mustPut(t, e, "hello.txt", "hello", "text/plain")
	rec := e.request(e.b, "GET", "/v1/objects/"+v.VersionID, nil, nil)
	if rec.Code != 404 || errCode(t, rec) != "version_not_found" {
		t.Fatalf("got %d %s", rec.Code, rec.Body)
	}
}

func TestRead_UnknownVersionIs404(t *testing.T) {
	e := setup(t)
	rec := e.read("GET", uuid.NewString(), nil)
	if rec.Code != 404 || errCode(t, rec) != "version_not_found" {
		t.Fatalf("got %d %s", rec.Code, rec.Body)
	}
}

func TestRead_MalformedIDIs404(t *testing.T) {
	e := setup(t)
	rec := e.read("GET", "not-a-uuid", nil)
	if rec.Code != 404 || errCode(t, rec) != "version_not_found" {
		t.Fatalf("got %d %s", rec.Code, rec.Body)
	}
}

func TestRead_MissingFileIs500(t *testing.T) {
	e := setup(t)
	v := mustPut(t, e, "hello.txt", "hello", "text/plain")
	if err := os.Remove(e.blobPath(v.VersionID)); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })

	rec := e.read("GET", v.VersionID, nil)
	if rec.Code != 500 || errCode(t, rec) != "internal_error" {
		t.Fatalf("got %d %s", rec.Code, rec.Body)
	}
	if !strings.Contains(buf.String(), "blob not found") {
		t.Fatalf("error not logged: %q", buf.String())
	}
}
