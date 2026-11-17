package docs_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"archio/internal/modules/admin"
	"archio/internal/modules/buckets"
	"archio/internal/modules/docs"
	"archio/internal/modules/objects"
	"archio/internal/server"

	"gopkg.in/yaml.v3"
)

func passthrough(h http.Handler) http.Handler { return h }

// appMux mounts the same routes as internal/app, without real dependencies.
func appMux() *http.ServeMux {
	return server.NewMux(http.NotFoundHandler(),
		func(m *http.ServeMux) { admin.RegisterRoutes(m, nil, passthrough) },
		func(m *http.ServeMux) { buckets.RegisterRoutes(m, nil, passthrough) },
		func(m *http.ServeMux) { objects.RegisterRoutes(m, objects.Deps{}, passthrough) },
		docs.RegisterRoutes,
	)
}

func get(t *testing.T, mux http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

func TestDocs_ServesSpecAndUI(t *testing.T) {
	mux := appMux()

	spec := get(t, mux, "/openapi.yaml")
	if spec.Code != http.StatusOK || !strings.HasPrefix(spec.Header().Get("Content-Type"), "application/yaml") {
		t.Fatalf("spec: status %d, content-type %q", spec.Code, spec.Header().Get("Content-Type"))
	}
	var doc map[string]any
	if err := yaml.Unmarshal(spec.Body.Bytes(), &doc); err != nil {
		t.Fatalf("spec is not valid YAML: %v", err)
	}
	if doc["openapi"] != "3.1.0" {
		t.Fatalf("openapi version = %v", doc["openapi"])
	}

	ui := get(t, mux, "/docs")
	if ui.Code != http.StatusOK || !strings.Contains(ui.Body.String(), `url: "/openapi.yaml"`) {
		t.Fatalf("docs page: status %d, body missing spec url", ui.Code)
	}
}

// Every operation in the spec must match a real route, and every real /v1 route must be documented.
func TestDocs_SpecMatchesRoutes(t *testing.T) {
	var doc struct {
		Paths map[string]map[string]any `yaml:"paths"`
	}
	if err := yaml.Unmarshal([]byte(get(t, appMux(), "/openapi.yaml").Body.String()), &doc); err != nil {
		t.Fatal(err)
	}

	samples := strings.NewReplacer(
		"{groupId}", "6f1c2f3e-0000-4000-8000-000000000001",
		"{keyId}", "6f1c2f3e-0000-4000-8000-000000000002",
		"{versionId}", "6f1c2f3e-0000-4000-8000-000000000003",
		"{bucket}", "my-bucket",
		"{key}", "photos/a.jpg",
	)
	mux := appMux()
	documented := map[string]bool{}
	for path, ops := range doc.Paths {
		for method := range ops {
			if method == "parameters" {
				continue
			}
			m := strings.ToUpper(method)
			_, pattern := mux.Handler(httptest.NewRequest(m, samples.Replace(path), nil))
			if pattern == "" {
				t.Errorf("%s %s is documented but no route serves it", m, path)
				continue
			}
			documented[pattern] = true
			if m == http.MethodHead {
				documented[strings.Replace(pattern, "GET ", "HEAD ", 1)] = true
			}
		}
	}

	served := []string{
		"POST /v1/admin/groups", "GET /v1/admin/groups", "DELETE /v1/admin/groups/{groupId}",
		"POST /v1/admin/groups/{groupId}/keys", "GET /v1/admin/groups/{groupId}/keys",
		"DELETE /v1/admin/groups/{groupId}/keys/{keyId}",
		"POST /v1/buckets", "GET /v1/buckets", "GET /v1/buckets/{bucket}", "DELETE /v1/buckets/{bucket}",
		"GET /v1/buckets/{bucket}/objects", "PUT /v1/buckets/{bucket}/objects/{key...}",
		"DELETE /v1/buckets/{bucket}/objects/{key...}", "GET /v1/buckets/{bucket}/versions/{key...}",
		"GET /v1/objects/{versionId}",
	}
	for _, p := range served {
		if !documented[p] {
			t.Errorf("route %q is not documented in openapi.yaml", p)
		}
	}
}
