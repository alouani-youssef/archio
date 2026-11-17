package app_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"archio/internal/app"
	"archio/internal/config"
	"archio/internal/testutil"
)

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func portOr(key string, fallback int) int {
	if n, err := strconv.Atoi(os.Getenv(key)); err == nil && n > 0 {
		return n
	}
	return fallback
}

func requireReachable(t *testing.T, name, host string, port int) {
	t.Helper()
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(host, strconv.Itoa(port)), 2*time.Second)
	if err != nil {
		t.Skipf("%s unavailable: %v", name, err)
	}
	_ = conn.Close()
}

func testConfig(t *testing.T) config.Config {
	t.Helper()
	cfg := config.Config{
		App: config.AppConfig{Host: "127.0.0.1", Port: 3000},
		DB: config.DBConfig{
			Host:     envOr("DB_HOST", "127.0.0.1"),
			Port:     portOr("DB_PORT", 5432),
			Username: envOr("DB_USERNAME", "archio_user"),
			Password: envOr("DB_PASSWORD", "archio_local_password"),
			Database: envOr("DB_DATABASE", "archio"),
		},
		Redis: config.RedisConfig{
			Host:     envOr("REDIS_HOST", "127.0.0.1"),
			Port:     portOr("REDIS_PORT", 6379),
			Password: envOr("REDIS_PASSWORD", "redis_local_password"),
		},
		Broker: config.BrokerConfig{
			Host:     envOr("BROKER_HOST", "127.0.0.1"),
			Port:     portOr("BROKER_PORT", 5672),
			Username: envOr("BROKER_USERNAME", "archio"),
			Password: envOr("BROKER_PASSWORD", "rabbitmq_local_password"),
		},
		Auth: config.AuthConfig{RootUsername: "root-e2e", RootPassword: "root-e2e-pass"},
		Storage: config.StorageConfig{
			Path:           t.TempDir(),
			PublicBaseURL:  "http://archio.test",
			MaxUploadBytes: 1 << 20,
		},
	}
	requireReachable(t, "postgres", cfg.DB.Host, cfg.DB.Port)
	requireReachable(t, "redis", cfg.Redis.Host, cfg.Redis.Port)
	requireReachable(t, "rabbitmq", cfg.Broker.Host, cfg.Broker.Port)
	return cfg
}

type client struct {
	t    *testing.T
	base string
}

func (c client) do(method, path, user, pass, bearer, contentType string, body []byte) (int, []byte) {
	c.t.Helper()
	req, err := http.NewRequest(method, c.base+path, bytes.NewReader(body))
	if err != nil {
		c.t.Fatal(err)
	}
	if user != "" {
		req.SetBasicAuth(user, pass)
	}
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, out
}

func (c client) admin(method, path string, payload any) (int, map[string]any) {
	c.t.Helper()
	var body []byte
	if payload != nil {
		body, _ = json.Marshal(payload)
	}
	status, out := c.do(method, path, "root-e2e", "root-e2e-pass", "", "application/json", body)
	var m map[string]any
	_ = json.Unmarshal(out, &m)
	return status, m
}

func (c client) newKey(group string) string {
	c.t.Helper()
	status, g := c.admin("POST", "/v1/admin/groups", map[string]string{"name": testutil.Unique(group)})
	if status != http.StatusCreated {
		c.t.Fatalf("create group: %d %v", status, g)
	}
	status, k := c.admin("POST", "/v1/admin/groups/"+g["id"].(string)+"/keys", map[string]string{"name": "e2e"})
	if status != http.StatusCreated {
		c.t.Fatalf("create key: %d %v", status, k)
	}
	return k["key"].(string)
}

func TestApp_EndToEnd(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cfg := testConfig(t)
	a, err := app.New(ctx, cfg)
	if err != nil {
		t.Fatalf("app.New: %v", err)
	}
	defer a.Close()

	srv := httptest.NewServer(a.Handler())
	defer srv.Close()
	c := client{t: t, base: srv.URL}

	keyA := c.newKey("e2e-a")
	keyB := c.newKey("e2e-b")

	bucket := testutil.Unique("e2e")
	if status, out := c.do("POST", "/v1/buckets", "", "", keyA, "application/json", []byte(`{"name":"`+bucket+`"}`)); status != http.StatusCreated {
		t.Fatalf("create bucket: %d %s", status, out)
	}

	var readURLs []string
	for _, content := range []string{"v1", "v2"} {
		status, out := c.do("PUT", "/v1/buckets/"+bucket+"/objects/doc.txt", "", "", keyA, "text/plain", []byte(content))
		if status != http.StatusCreated {
			t.Fatalf("upload %s: %d %s", content, status, out)
		}
		var v struct {
			ReadURL string `json:"readUrl"`
		}
		if err := json.Unmarshal(out, &v); err != nil || v.ReadURL == "" {
			t.Fatalf("decode upload response: %v %s", err, out)
		}
		if !strings.HasPrefix(v.ReadURL, cfg.Storage.PublicBaseURL+"/v1/objects/") {
			t.Fatalf("readUrl %q does not use the public base URL", v.ReadURL)
		}
		readURLs = append(readURLs, v.ReadURL)
	}

	pathOf := func(readURL string) string {
		u, err := url.Parse(readURL)
		if err != nil {
			t.Fatal(err)
		}
		return u.Path
	}
	for i, want := range []string{"v1", "v2"} {
		status, body := c.do("GET", pathOf(readURLs[i]), "", "", keyA, "", nil)
		if status != http.StatusOK || string(body) != want {
			t.Fatalf("read version %d: status %d body %q, want %q", i+1, status, body, want)
		}
		if status, _ := c.do("GET", pathOf(readURLs[i]), "", "", "", "", nil); status != http.StatusUnauthorized {
			t.Fatalf("read without key: status %d, want 401", status)
		}
		if status, _ := c.do("GET", pathOf(readURLs[i]), "", "", keyB, "", nil); status != http.StatusNotFound {
			t.Fatalf("read with other group's key: status %d, want 404", status)
		}
	}

	if status, body := c.do("GET", "/health", "", "", "", "", nil); status != http.StatusOK {
		t.Fatalf("health: %d %s", status, body)
	}
}
