package objects

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"archio/internal/auth"
	"archio/internal/ent"
	"archio/internal/ent/bucket"
	"archio/internal/ent/metadata"
	"archio/internal/ent/object"
	"archio/internal/ent/objectversion"
	"archio/internal/events"
	"archio/internal/storage"
	"archio/internal/testutil"
	"github.com/google/uuid"
)

const helloSHA = "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824"

// fakeAuth injects the group named by the X-Group header.
func fakeAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if g := r.Header.Get("X-Group"); g != "" {
			r = r.WithContext(auth.WithGroup(r.Context(), uuid.MustParse(g)))
		}
		next.ServeHTTP(w, r)
	})
}

type published struct {
	routingKey string
	payload    any
}

type fakePublisher struct {
	mu     sync.Mutex
	events []published
	err    error
}

func (p *fakePublisher) Publish(_ context.Context, routingKey string, payload any) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.events = append(p.events, published{routingKey, payload})
	return p.err
}

type env struct {
	client *ent.Client
	h      http.Handler
	root   string
	pub    *fakePublisher
	a, b   *ent.Group
	bkt    *ent.Bucket // belongs to a
	other  *ent.Bucket // belongs to b
}

type option func(*Deps)

func setup(t *testing.T, opts ...option) *env {
	t.Helper()
	client := testutil.OpenDB(t)
	root := t.TempDir()
	st, err := storage.NewLocal(root)
	if err != nil {
		t.Fatal(err)
	}
	e := &env{client: client, root: root, pub: &fakePublisher{}}
	d := Deps{Client: client, Storage: st, Publisher: e.pub, PublicBaseURL: "http://files.test", MaxUploadBytes: 1 << 20}
	for _, o := range opts {
		o(&d)
	}
	mux := http.NewServeMux()
	RegisterRoutes(mux, d, fakeAuth)
	e.h = mux

	ctx := context.Background()
	if e.a, err = client.Group.Create().SetName(testutil.Unique("grp")).Save(ctx); err != nil {
		t.Fatal(err)
	}
	if e.b, err = client.Group.Create().SetName(testutil.Unique("grp")).Save(ctx); err != nil {
		t.Fatal(err)
	}
	if e.bkt, err = client.Bucket.Create().SetName(testutil.Unique("bkt")).SetGroup(e.a).Save(ctx); err != nil {
		t.Fatal(err)
	}
	if e.other, err = client.Bucket.Create().SetName(testutil.Unique("bkt")).SetGroup(e.b).Save(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		for _, b := range []*ent.Bucket{e.bkt, e.other} {
			inBucket := object.HasBucketWith(bucket.ID(b.ID))
			_, _ = client.MetaData.Delete().Where(metadata.HasObjectWith(inBucket)).Exec(ctx)
			_, _ = client.ObjectVersion.Delete().Where(objectversion.HasObjectWith(inBucket)).Exec(ctx)
			_, _ = client.Object.Delete().Where(inBucket).Exec(ctx)
			_ = client.Bucket.DeleteOne(b).Exec(ctx)
		}
		_ = client.Group.DeleteOne(e.a).Exec(ctx)
		_ = client.Group.DeleteOne(e.b).Exec(ctx)
	})
	return e
}

// objectURL builds the object route for a plain (already URL-safe) key.
func objectURL(bucketName, key string) string {
	return "/v1/buckets/" + bucketName + "/objects/" + key
}

func (e *env) request(g *ent.Group, method, target string, body io.Reader, header http.Header) *httptest.ResponseRecorder {
	if body == nil {
		body = http.NoBody
	}
	req := httptest.NewRequest(method, target, body)
	for k, v := range header {
		req.Header[k] = v
	}
	if g != nil {
		req.Header.Set("X-Group", g.ID.String())
	}
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, req)
	return rec
}

func (e *env) put(t *testing.T, key, body, contentType string) *httptest.ResponseRecorder {
	t.Helper()
	h := http.Header{}
	if contentType != "" {
		h.Set("Content-Type", contentType)
	}
	return e.request(e.a, "PUT", objectURL(e.bkt.Name, key), strings.NewReader(body), h)
}

type versionJSON struct {
	VersionID      string `json:"versionId"`
	Key            string `json:"key"`
	Size           int64  `json:"size"`
	ChecksumSha256 string `json:"checksumSha256"`
	ContentType    string `json:"contentType"`
	CreatedAt      string `json:"createdAt"`
	ReadURL        string `json:"readUrl"`
}

type objectJSON struct {
	Key            string       `json:"key"`
	UpdatedAt      string       `json:"updatedAt"`
	CurrentVersion *versionJSON `json:"currentVersion"`
}

type listJSON struct {
	Objects    []objectJSON `json:"objects"`
	NextCursor *string      `json:"nextCursor"`
}

func decodeInto(t *testing.T, rec *httptest.ResponseRecorder, v any) {
	t.Helper()
	if err := json.Unmarshal(rec.Body.Bytes(), v); err != nil {
		t.Fatalf("decode %q: %v", rec.Body.String(), err)
	}
}

func mustPut(t *testing.T, e *env, key, body, contentType string) versionJSON {
	t.Helper()
	rec := e.put(t, key, body, contentType)
	if rec.Code != 201 {
		t.Fatalf("put %q: %d %s", key, rec.Code, rec.Body)
	}
	var v versionJSON
	decodeInto(t, rec, &v)
	return v
}

func errCode(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var body struct {
		Error struct{ Code string } `json:"error"`
	}
	decodeInto(t, rec, &body)
	return body.Error.Code
}

func (e *env) versions(t *testing.T, key string) []versionJSON {
	t.Helper()
	rec := e.request(e.a, "GET", "/v1/buckets/"+e.bkt.Name+"/versions/"+key, nil, nil)
	if rec.Code != 200 {
		t.Fatalf("versions %q: %d %s", key, rec.Code, rec.Body)
	}
	var out struct{ Versions []versionJSON }
	decodeInto(t, rec, &out)
	return out.Versions
}

func (e *env) list(t *testing.T, query string) listJSON {
	t.Helper()
	rec := e.request(e.a, "GET", "/v1/buckets/"+e.bkt.Name+"/objects?"+query, nil, nil)
	if rec.Code != 200 {
		t.Fatalf("list: %d %s", rec.Code, rec.Body)
	}
	var out listJSON
	decodeInto(t, rec, &out)
	return out
}

func (e *env) objectCount(t *testing.T) int {
	t.Helper()
	n, err := e.client.Object.Query().Where(object.HasBucketWith(bucket.ID(e.bkt.ID))).Count(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func assertTmpEmpty(t *testing.T, root string) {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(root, ".tmp"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf(".tmp has %d entries, want 0", len(entries))
	}
}

func (e *env) blobPath(versionID string) string {
	return filepath.Join(e.root, storage.Key(e.a.ID, e.bkt.ID, uuid.MustParse(versionID)))
}

func TestUpload_CreatesVersion(t *testing.T) {
	e := setup(t)
	v := mustPut(t, e, "docs/hello.txt", "hello", "text/plain")
	if v.Key != "docs/hello.txt" || v.Size != 5 || v.ChecksumSha256 != helloSHA || v.ContentType != "text/plain" || v.CreatedAt == "" {
		t.Fatalf("bad version: %+v", v)
	}
	if v.ReadURL != "http://files.test/v1/objects/"+v.VersionID {
		t.Fatalf("readUrl = %q", v.ReadURL)
	}
	b, err := os.ReadFile(e.blobPath(v.VersionID))
	if err != nil || string(b) != "hello" {
		t.Fatalf("stored file: %q %v", b, err)
	}
	if len(e.pub.events) != 1 {
		t.Fatalf("published %d events, want 1", len(e.pub.events))
	}
	ev := e.pub.events[0]
	got, ok := ev.payload.(events.ObjectVersionCreatedEvent)
	if ev.routingKey != events.ObjectVersionCreated || !ok {
		t.Fatalf("bad event: %+v", ev)
	}
	if got.VersionID.String() != v.VersionID || got.ObjectKey != v.Key || got.BucketID != e.bkt.ID ||
		got.BucketName != e.bkt.Name || got.GroupID != e.a.ID || got.Size != 5 ||
		got.ChecksumSHA256 != helloSHA || got.ContentType != "text/plain" || got.CreatedAt.IsZero() {
		t.Fatalf("event fields: %+v", got)
	}
}

func TestUpload_DefaultContentTypeAndEmptyBody(t *testing.T) {
	e := setup(t)
	v := mustPut(t, e, "empty", "", "")
	if v.ContentType != "application/octet-stream" || v.Size != 0 {
		t.Fatalf("bad version: %+v", v)
	}
}

func TestUpload_ReuploadAddsVersion(t *testing.T) {
	e := setup(t)
	v1 := mustPut(t, e, "k", "one", "text/plain")
	v2 := mustPut(t, e, "k", "two", "text/plain")
	if v1.VersionID == v2.VersionID {
		t.Fatal("same version id twice")
	}
	vs := e.versions(t, "k")
	if len(vs) != 2 || vs[0].VersionID != v2.VersionID || vs[1].VersionID != v1.VersionID {
		t.Fatalf("versions: %+v", vs)
	}
	l := e.list(t, "")
	if len(l.Objects) != 1 || l.Objects[0].CurrentVersion == nil || l.Objects[0].CurrentVersion.VersionID != v2.VersionID {
		t.Fatalf("list: %+v", l)
	}
}

func TestUpload_UnicodeKeyRoundTrip(t *testing.T) {
	e := setup(t)
	const want = "photos/été 2026/a b.jpg"
	rec := e.request(e.a, "PUT", "/v1/buckets/"+e.bkt.Name+"/objects/photos/%C3%A9t%C3%A9%202026/a%20b.jpg", strings.NewReader("x"), nil)
	if rec.Code != 201 {
		t.Fatalf("put: %d %s", rec.Code, rec.Body)
	}
	var v versionJSON
	decodeInto(t, rec, &v)
	if v.Key != want {
		t.Fatalf("key = %q", v.Key)
	}
	if l := e.list(t, ""); len(l.Objects) != 1 || l.Objects[0].Key != want {
		t.Fatalf("list: %+v", l)
	}
	vs := e.versions(t, "photos/%C3%A9t%C3%A9%202026/a%20b.jpg")
	if len(vs) != 1 || vs[0].Key != want {
		t.Fatalf("versions: %+v", vs)
	}
}

func TestUpload_TooLarge(t *testing.T) {
	e := setup(t, func(d *Deps) { d.MaxUploadBytes = 4 })
	rec := e.put(t, "big", "hello", "")
	if rec.Code != 413 || errCode(t, rec) != "payload_too_large" {
		t.Fatalf("got %d %s", rec.Code, rec.Body)
	}
	if n := e.objectCount(t); n != 0 {
		t.Fatalf("%d object rows, want 0", n)
	}
	assertTmpEmpty(t, e.root)
}

// abortReader yields data, then fails as if the client disconnected.
type abortReader struct{ data []byte }

func (a *abortReader) Read(p []byte) (int, error) {
	if len(a.data) == 0 {
		return 0, errors.New("client disconnected")
	}
	n := copy(p, a.data)
	a.data = a.data[n:]
	return n, nil
}

func TestUpload_ClientAbort(t *testing.T) {
	e := setup(t)
	v1 := mustPut(t, e, "k", "first", "")
	rec := e.request(e.a, "PUT", objectURL(e.bkt.Name, "k"), &abortReader{data: []byte("abc")}, nil)
	if rec.Code < 400 {
		t.Fatalf("aborted upload succeeded: %d %s", rec.Code, rec.Body)
	}
	if vs := e.versions(t, "k"); len(vs) != 1 || vs[0].VersionID != v1.VersionID {
		t.Fatalf("versions: %+v", vs)
	}
	if l := e.list(t, ""); len(l.Objects) != 1 || l.Objects[0].CurrentVersion.VersionID != v1.VersionID {
		t.Fatalf("current version changed: %+v", l)
	}
	assertTmpEmpty(t, e.root)
}

func TestUpload_ConcurrentSameNewKey(t *testing.T) {
	e := setup(t)
	var wg sync.WaitGroup
	recs := make([]*httptest.ResponseRecorder, 2)
	for i := range recs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			recs[i] = e.request(e.a, "PUT", objectURL(e.bkt.Name, "race"), strings.NewReader("x"), nil)
		}()
	}
	wg.Wait()
	ids := map[string]bool{}
	for _, rec := range recs {
		if rec.Code != 201 {
			t.Fatalf("put: %d %s", rec.Code, rec.Body)
		}
		var v versionJSON
		decodeInto(t, rec, &v)
		ids[v.VersionID] = true
	}
	if len(ids) != 2 {
		t.Fatalf("version ids not distinct: %v", ids)
	}
	if n := e.objectCount(t); n != 1 {
		t.Fatalf("%d object rows, want 1", n)
	}
	if vs := e.versions(t, "race"); len(vs) != 2 {
		t.Fatalf("versions: %+v", vs)
	}
}

func TestUpload_PublishFailureStillSucceeds(t *testing.T) {
	e := setup(t)
	e.pub.err = errors.New("broker down")
	if rec := e.put(t, "k", "x", ""); rec.Code != 201 {
		t.Fatalf("got %d %s", rec.Code, rec.Body)
	}
}

func TestUpload_InvalidKey(t *testing.T) {
	e := setup(t)
	// ServeMux redirects a literal "/./" path, so the dot segment is percent-encoded
	// to reach the handler with the key "a/./b".
	rec := e.request(e.a, "PUT", "/v1/buckets/"+e.bkt.Name+"/objects/a/%2E/b", strings.NewReader("x"), nil)
	if rec.Code != 400 || errCode(t, rec) != "invalid_object_key" {
		t.Fatalf("got %d %s", rec.Code, rec.Body)
	}
}

func TestUpload_UnknownBucket(t *testing.T) {
	e := setup(t)
	rec := e.request(e.a, "PUT", objectURL(testutil.Unique("nope"), "k"), strings.NewReader("x"), nil)
	if rec.Code != 404 || errCode(t, rec) != "bucket_not_found" {
		t.Fatalf("got %d %s", rec.Code, rec.Body)
	}
}

func TestUpload_OtherGroupBucket(t *testing.T) {
	e := setup(t)
	rec := e.request(e.a, "PUT", objectURL(e.other.Name, "k"), strings.NewReader("x"), nil)
	if rec.Code != 404 || errCode(t, rec) != "bucket_not_found" {
		t.Fatalf("got %d %s", rec.Code, rec.Body)
	}
}

func TestUpload_NoGroupIs401(t *testing.T) {
	e := setup(t)
	rec := e.request(nil, "PUT", objectURL(e.bkt.Name, "k"), strings.NewReader("x"), nil)
	if rec.Code != 401 || errCode(t, rec) != "unauthorized" {
		t.Fatalf("got %d %s", rec.Code, rec.Body)
	}
}

func keysOf(l listJSON) []string {
	var ks []string
	for _, o := range l.Objects {
		ks = append(ks, o.Key)
	}
	return ks
}

func TestList_PrefixAndPagination(t *testing.T) {
	e := setup(t)
	for _, k := range []string{"a/3", "b/1", "a/1", "a/2"} {
		mustPut(t, e, k, "x", "")
	}
	p1 := e.list(t, "prefix=a/&limit=2")
	if ks := keysOf(p1); len(ks) != 2 || ks[0] != "a/1" || ks[1] != "a/2" || p1.NextCursor == nil {
		t.Fatalf("page 1: %+v", p1)
	}
	p2 := e.list(t, "prefix=a/&limit=2&cursor="+url.QueryEscape(*p1.NextCursor))
	if ks := keysOf(p2); len(ks) != 1 || ks[0] != "a/3" || p2.NextCursor != nil {
		t.Fatalf("page 2: %+v", p2)
	}
	for _, limit := range []string{"0", "1001"} {
		rec := e.request(e.a, "GET", "/v1/buckets/"+e.bkt.Name+"/objects?limit="+limit, nil, nil)
		if rec.Code != 400 || errCode(t, rec) != "invalid_limit" {
			t.Fatalf("limit=%s: %d %s", limit, rec.Code, rec.Body)
		}
	}
}

func TestDelete_RemovesVersionsAndFiles(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	v1 := mustPut(t, e, "k", "one", "")
	v2 := mustPut(t, e, "k", "two", "")
	o, err := e.client.Object.Query().Where(object.Identifier("k"), object.HasBucketWith(bucket.ID(e.bkt.ID))).Only(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.client.MetaData.Create().SetPropertyIri("https://schema.org/name").SetObject(o).Save(ctx); err != nil {
		t.Fatal(err)
	}

	if rec := e.request(e.a, "DELETE", objectURL(e.bkt.Name, "k"), nil, nil); rec.Code != 204 {
		t.Fatalf("delete: %d %s", rec.Code, rec.Body)
	}
	rec := e.request(e.a, "GET", "/v1/buckets/"+e.bkt.Name+"/versions/k", nil, nil)
	if rec.Code != 404 || errCode(t, rec) != "object_not_found" {
		t.Fatalf("versions after delete: %d %s", rec.Code, rec.Body)
	}
	for _, v := range []versionJSON{v1, v2} {
		if _, err := os.Stat(e.blobPath(v.VersionID)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("file for %s still exists: %v", v.VersionID, err)
		}
	}
	if n, err := e.client.MetaData.Query().Where(metadata.HasObjectWith(object.ID(o.ID))).Count(ctx); err != nil || n != 0 {
		t.Fatalf("metadata rows = %d, %v", n, err)
	}
	if rec := e.request(e.a, "DELETE", objectURL(e.bkt.Name, "k"), nil, nil); rec.Code != 404 || errCode(t, rec) != "object_not_found" {
		t.Fatalf("second delete: %d %s", rec.Code, rec.Body)
	}
}
