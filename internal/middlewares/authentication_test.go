package middlewares

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"archio/internal/auth"
	"github.com/google/uuid"
)

func TestBasicAuth(t *testing.T) {
	called := false
	h := BasicAuth("root", "pw")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusNoContent)
	}))
	cases := []struct {
		name, u, p string
		set        bool
	}{{"none", "", "", false}, {"wrong user", "x", "pw", true}, {"wrong pass", "root", "x", true}}
	for _, c := range cases {
		req := httptest.NewRequest("GET", "/", nil)
		if c.set {
			req.SetBasicAuth(c.u, c.p)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != 401 || rec.Header().Get("WWW-Authenticate") != `Basic realm="archio"` {
			t.Fatalf("%s: code=%d header=%q", c.name, rec.Code, rec.Header().Get("WWW-Authenticate"))
		}
	}
	if called {
		t.Fatal("next called on bad credentials")
	}
	req := httptest.NewRequest("GET", "/", nil)
	req.SetBasicAuth("root", "pw")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 204 || !called {
		t.Fatalf("code=%d called=%v", rec.Code, called)
	}
}

type fakeAuth struct{ id uuid.UUID }

func (f fakeAuth) Authenticate(ctx context.Context, hash string) (uuid.UUID, error) {
	return f.id, nil
}

func TestAPIKeyAuth(t *testing.T) {
	g := uuid.New()
	var got uuid.UUID
	var ok bool
	h := APIKeyAuth(fakeAuth{g})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, ok = auth.GroupFrom(r.Context())
	}))
	for _, hdr := range []string{"", "Basic abc", "Bearer"} {
		req := httptest.NewRequest("GET", "/", nil)
		if hdr != "" {
			req.Header.Set("Authorization", hdr)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		var body struct{ Error struct{ Code string } }
		_ = json.NewDecoder(rec.Body).Decode(&body)
		if rec.Code != 401 || body.Error.Code != "unauthorized" {
			t.Fatalf("%q: code=%d body=%+v", hdr, rec.Code, body)
		}
	}
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer ak_whatever")
	h.ServeHTTP(httptest.NewRecorder(), req)
	if !ok || got != g {
		t.Fatalf("group = %v ok=%v", got, ok)
	}
}
