package admin

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
	"archio/internal/ent/apikey"
	"archio/internal/ent/group"
	"archio/internal/testutil"
	apperr "archio/pkg/errors"
)

func passthrough(next http.Handler) http.Handler { return next }

func setup(t *testing.T) (*ent.Client, http.Handler) {
	t.Helper()
	client := testutil.OpenDB(t)
	mux := http.NewServeMux()
	RegisterRoutes(mux, client, passthrough)
	return client, mux
}

func do(h http.Handler, method, path string, body any) *httptest.ResponseRecorder {
	var buf bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&buf).Encode(body)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(method, path, &buf))
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

func TestAdmin_GroupAndKeyLifecycle(t *testing.T) {
	client, h := setup(t)
	ctx := context.Background()
	name := testutil.Unique("grp")

	rec := do(h, "POST", "/v1/admin/groups", map[string]string{"name": name})
	if rec.Code != 201 {
		t.Fatalf("create group: %d %s", rec.Code, rec.Body)
	}
	gid := decode(t, rec)["id"].(string)
	t.Cleanup(func() { do(h, "DELETE", "/v1/admin/groups/"+gid, nil) })

	if rec = do(h, "POST", "/v1/admin/groups", map[string]string{"name": name}); rec.Code != 409 || errCode(t, rec) != "group_exists" {
		t.Fatalf("dup group: %d %s", rec.Code, rec.Body)
	}

	rec = do(h, "POST", "/v1/admin/groups/"+gid+"/keys", map[string]string{"name": "ci"})
	if rec.Code != 201 {
		t.Fatalf("create key: %d %s", rec.Code, rec.Body)
	}
	created := decode(t, rec)
	key, _ := created["key"].(string)
	keyID := created["id"].(string)
	if !strings.HasPrefix(key, "ak_") {
		t.Fatalf("missing plaintext key: %v", created)
	}

	rec = do(h, "GET", "/v1/admin/groups/"+gid+"/keys", nil)
	if rec.Code != 200 {
		t.Fatalf("list keys: %d", rec.Code)
	}
	raw := rec.Body.String()
	if strings.Contains(raw, key) || strings.Contains(strings.ToLower(raw), "hash") || strings.Contains(raw, `"key"`) {
		t.Fatalf("list leaks secret material: %s", raw)
	}
	var list []map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &list)
	if len(list) != 1 || list[0]["prefix"] != key[:11] {
		t.Fatalf("unexpected list: %s", raw)
	}

	a := NewKeyAuthenticator(client)
	got, err := a.Authenticate(ctx, auth.HashKey(key))
	if err != nil || got.String() != gid {
		t.Fatalf("authenticate: %v %v", got, err)
	}

	if rec = do(h, "DELETE", "/v1/admin/groups/"+gid+"/keys/"+keyID, nil); rec.Code != 204 {
		t.Fatalf("revoke: %d", rec.Code)
	}
	_, err = a.Authenticate(ctx, auth.HashKey(key))
	var appErr *apperr.Error
	if !asAppErr(err, &appErr) || appErr.Status != 401 {
		t.Fatalf("revoked key should give 401, got %v", err)
	}
	if _, err = a.Authenticate(ctx, auth.HashKey("ak_unknown")); err == nil {
		t.Fatal("unknown key accepted")
	}
}

func asAppErr(err error, target **apperr.Error) bool {
	e, ok := err.(*apperr.Error)
	if ok {
		*target = e
	}
	return ok
}

func TestAdmin_DeleteGroupWithBuckets(t *testing.T) {
	client, h := setup(t)
	ctx := context.Background()

	g, err := client.Group.Create().SetName(testutil.Unique("grp")).Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	b, err := client.Bucket.Create().SetName(testutil.Unique("bkt")).SetGroup(g).Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = client.Bucket.DeleteOne(b).Exec(ctx)
		do(h, "DELETE", "/v1/admin/groups/"+g.ID.String(), nil)
	})

	rec := do(h, "DELETE", "/v1/admin/groups/"+g.ID.String(), nil)
	if rec.Code != 409 || errCode(t, rec) != "group_not_empty" {
		t.Fatalf("got %d %s", rec.Code, rec.Body)
	}

	empty, err := client.Group.Create().SetName(testutil.Unique("grp")).Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	rec = do(h, "POST", "/v1/admin/groups/"+empty.ID.String()+"/keys", map[string]string{"name": "k"})
	if rec.Code != 201 {
		t.Fatalf("create key: %d", rec.Code)
	}
	if rec = do(h, "DELETE", "/v1/admin/groups/"+empty.ID.String(), nil); rec.Code != 204 {
		t.Fatalf("delete empty: %d %s", rec.Code, rec.Body)
	}
	if exists, _ := (&repository{client: client}).groupExists(ctx, empty.ID); exists {
		t.Fatal("group still exists")
	}
	orphans, err := client.ApiKey.Query().Where(apikey.HasGroupWith(group.ID(empty.ID))).Count(ctx)
	if err != nil || orphans != 0 {
		t.Fatalf("keys remain: %d %v", orphans, err)
	}
}

func TestAdmin_UnknownGroup(t *testing.T) {
	_, h := setup(t)
	for _, id := range []string{"00000000-0000-4000-8000-000000000001", "not-a-uuid"} {
		for _, c := range []struct{ method, path string }{
			{"POST", "/v1/admin/groups/" + id + "/keys"},
			{"GET", "/v1/admin/groups/" + id + "/keys"},
			{"DELETE", "/v1/admin/groups/" + id + "/keys/" + id},
		} {
			rec := do(h, c.method, c.path, map[string]string{"name": "k"})
			if rec.Code != 404 || errCode(t, rec) != "group_not_found" {
				t.Fatalf("%s %s: %d %s", c.method, c.path, rec.Code, rec.Body)
			}
		}
	}
}
