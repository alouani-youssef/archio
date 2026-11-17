package buckets

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"archio/internal/auth"
	"archio/internal/ent"
	"archio/internal/ent/bucket"
	"archio/internal/ent/object"
	"archio/internal/testutil"
	"github.com/google/uuid"
)

func TestValidateName(t *testing.T) {
	valid := []string{"abc", "my.bucket-1", strings.Repeat("a", 63)}
	invalid := []string{"ab", strings.Repeat("a", 64), "Abc", "-abc", "abc-", "a_b"}
	for _, n := range valid {
		if err := ValidateName(n); err != nil {
			t.Errorf("%q should be valid: %v", n, err)
		}
	}
	for _, n := range invalid {
		if err := ValidateName(n); err == nil {
			t.Errorf("%q should be invalid", n)
		}
	}
}

// fakeAuth injects the group named by the X-Group header.
func fakeAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if g := r.Header.Get("X-Group"); g != "" {
			r = r.WithContext(auth.WithGroup(r.Context(), uuid.MustParse(g)))
		}
		next.ServeHTTP(w, r)
	})
}

type env struct {
	client *ent.Client
	h      http.Handler
	a, b   *ent.Group
}

func setup(t *testing.T) *env {
	t.Helper()
	client := testutil.OpenDB(t)
	mux := http.NewServeMux()
	RegisterRoutes(mux, client, fakeAuth)
	ctx := context.Background()
	e := &env{client: client, h: mux}
	var err error
	if e.a, err = client.Group.Create().SetName(testutil.Unique("grp")).Save(ctx); err != nil {
		t.Fatal(err)
	}
	if e.b, err = client.Group.Create().SetName(testutil.Unique("grp")).Save(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		for _, g := range []*ent.Group{e.a, e.b} {
			bs, _ := g.QueryBuckets().All(ctx)
			for _, b := range bs {
				_, _ = client.Object.Delete().Where(object.HasBucketWith(bucket.ID(b.ID))).Exec(ctx)
				_ = client.Bucket.DeleteOne(b).Exec(ctx)
			}
			_ = client.Group.DeleteOne(g).Exec(ctx)
		}
	})
	return e
}

func (e *env) do(g *ent.Group, method, path string, body any) *httptest.ResponseRecorder {
	var buf bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&buf).Encode(body)
	}
	req := httptest.NewRequest(method, path, &buf)
	if g != nil {
		req.Header.Set("X-Group", g.ID.String())
	}
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, req)
	return rec
}

func decode(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
		t.Fatalf("decode %q: %v", rec.Body.String(), err)
	}
	return m
}

func errCode(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	e, _ := decode(t, rec)["error"].(map[string]any)
	code, _ := e["code"].(string)
	return code
}

func listNames(t *testing.T, rec *httptest.ResponseRecorder) []string {
	t.Helper()
	var out struct {
		Buckets []struct{ Name string } `json:"buckets"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, b := range out.Buckets {
		names = append(names, b.Name)
	}
	return names
}

func contains(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}

func TestBuckets_CRUD(t *testing.T) {
	e := setup(t)
	name := testutil.Unique("bkt")

	rec := e.do(e.a, "POST", "/v1/buckets", map[string]string{"name": name, "description": "d"})
	if rec.Code != 201 {
		t.Fatalf("create: %d %s", rec.Code, rec.Body)
	}
	if m := decode(t, rec); m["name"] != name || m["description"] != "d" || m["createdAt"] == nil {
		t.Fatalf("bad body: %v", m)
	}
	if rec = e.do(e.a, "GET", "/v1/buckets/"+name, nil); rec.Code != 200 || decode(t, rec)["name"] != name {
		t.Fatalf("get: %d %s", rec.Code, rec.Body)
	}
	if rec = e.do(e.a, "GET", "/v1/buckets", nil); rec.Code != 200 || !contains(listNames(t, rec), name) {
		t.Fatalf("list: %d %s", rec.Code, rec.Body)
	}
	if rec = e.do(e.a, "DELETE", "/v1/buckets/"+name, nil); rec.Code != 204 {
		t.Fatalf("delete: %d %s", rec.Code, rec.Body)
	}
	if rec = e.do(e.a, "GET", "/v1/buckets/"+name, nil); rec.Code != 404 {
		t.Fatalf("get after delete: %d", rec.Code)
	}
}

func TestBuckets_ListSortedByName(t *testing.T) {
	e := setup(t)
	p := testutil.Unique("srt")
	for _, s := range []string{"-c", "-a", "-b"} {
		if rec := e.do(e.a, "POST", "/v1/buckets", map[string]string{"name": p + s}); rec.Code != 201 {
			t.Fatalf("create: %d %s", rec.Code, rec.Body)
		}
	}
	names := listNames(t, e.do(e.a, "GET", "/v1/buckets", nil))
	if len(names) != 3 || names[0] != p+"-a" || names[1] != p+"-b" || names[2] != p+"-c" {
		t.Fatalf("not sorted: %v", names)
	}
}

func TestBuckets_NameUniqueAcrossGroups(t *testing.T) {
	e := setup(t)
	name := testutil.Unique("bkt")
	if rec := e.do(e.a, "POST", "/v1/buckets", map[string]string{"name": name}); rec.Code != 201 {
		t.Fatalf("create: %d %s", rec.Code, rec.Body)
	}
	rec := e.do(e.b, "POST", "/v1/buckets", map[string]string{"name": name})
	if rec.Code != 409 || errCode(t, rec) != "bucket_exists" {
		t.Fatalf("got %d %s", rec.Code, rec.Body)
	}
}

func TestBuckets_CrossGroupIs404(t *testing.T) {
	e := setup(t)
	name := testutil.Unique("bkt")
	if rec := e.do(e.a, "POST", "/v1/buckets", map[string]string{"name": name}); rec.Code != 201 {
		t.Fatalf("create: %d %s", rec.Code, rec.Body)
	}
	for _, m := range []string{"GET", "DELETE"} {
		rec := e.do(e.b, m, "/v1/buckets/"+name, nil)
		if rec.Code != 404 || errCode(t, rec) != "bucket_not_found" {
			t.Fatalf("%s: %d %s", m, rec.Code, rec.Body)
		}
	}
	if contains(listNames(t, e.do(e.b, "GET", "/v1/buckets", nil)), name) {
		t.Fatal("B's list leaks A's bucket")
	}
	if rec := e.do(e.a, "GET", "/v1/buckets/"+name, nil); rec.Code != 200 {
		t.Fatalf("A lost bucket: %d", rec.Code)
	}
}

func TestBuckets_DeleteNonEmpty(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	name := testutil.Unique("bkt")
	if rec := e.do(e.a, "POST", "/v1/buckets", map[string]string{"name": name}); rec.Code != 201 {
		t.Fatalf("create: %d %s", rec.Code, rec.Body)
	}
	b, err := Resolve(ctx, e.client, e.a.ID, name)
	if err != nil {
		t.Fatal(err)
	}
	o, err := e.client.Object.Create().SetIdentifier("k").SetBucket(b).Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	rec := e.do(e.a, "DELETE", "/v1/buckets/"+name, nil)
	if rec.Code != 409 || errCode(t, rec) != "bucket_not_empty" {
		t.Fatalf("got %d %s", rec.Code, rec.Body)
	}
	if err := e.client.Object.DeleteOne(o).Exec(ctx); err != nil {
		t.Fatal(err)
	}
	if rec = e.do(e.a, "DELETE", "/v1/buckets/"+name, nil); rec.Code != 204 {
		t.Fatalf("delete after emptying: %d %s", rec.Code, rec.Body)
	}
}

func TestBuckets_InvalidName(t *testing.T) {
	e := setup(t)
	rec := e.do(e.a, "POST", "/v1/buckets", map[string]string{"name": "Bad_Name"})
	if rec.Code != 400 || errCode(t, rec) != "invalid_bucket_name" {
		t.Fatalf("got %d %s", rec.Code, rec.Body)
	}
}

func TestBuckets_NoGroupIs401(t *testing.T) {
	e := setup(t)
	rec := e.do(nil, "GET", "/v1/buckets", nil)
	if rec.Code != 401 || errCode(t, rec) != "unauthorized" {
		t.Fatalf("got %d %s", rec.Code, rec.Body)
	}
}
