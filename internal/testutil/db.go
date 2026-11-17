// Package testutil provides helpers shared by integration tests.
package testutil

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"net"
	"net/url"
	"os"
	"strconv"
	"testing"
	"time"

	"archio/internal/config"
	"archio/internal/database"
	"archio/internal/ent"
	_ "github.com/jackc/pgx/v5/stdlib"
)

// migrateLockID is the Postgres advisory lock that serializes migrations
// across test packages running in parallel against the same database.
const migrateLockID = 7243001

// OpenDB returns a migrated Ent client, skipping the test when Postgres is unreachable.
func OpenDB(t *testing.T) *ent.Client {
	t.Helper()

	cfg := dbConfig()

	pingCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	conn, err := database.Open(pingCtx, cfg)
	if err != nil {
		t.Skipf("postgres unavailable: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	unlock := lockMigrations(t, cfg)
	defer unlock()

	if err := conn.Migrate(context.Background()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return conn.Client()
}

// lockMigrations takes the advisory lock on a dedicated connection.
func lockMigrations(t *testing.T, cfg config.DBConfig) func() {
	t.Helper()
	lockDB, err := sql.Open("pgx", dsn(cfg))
	if err != nil {
		t.Fatalf("open lock connection: %v", err)
	}
	ctx := context.Background()
	c, err := lockDB.Conn(ctx)
	if err != nil {
		_ = lockDB.Close()
		t.Fatalf("acquire lock connection: %v", err)
	}
	if _, err := c.ExecContext(ctx, "SELECT pg_advisory_lock($1)", migrateLockID); err != nil {
		_ = c.Close()
		_ = lockDB.Close()
		t.Fatalf("advisory lock: %v", err)
	}
	return func() {
		_, _ = c.ExecContext(ctx, "SELECT pg_advisory_unlock($1)", migrateLockID)
		_ = c.Close()
		_ = lockDB.Close()
	}
}

// Unique returns prefix plus a random lowercase hex suffix.
func Unique(prefix string) string {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return prefix + "-" + hex.EncodeToString(b)
}

func dbConfig() config.DBConfig {
	port, err := strconv.Atoi(getenv("DB_PORT", "5432"))
	if err != nil {
		port = 5432
	}
	return config.DBConfig{
		Host:     getenv("DB_HOST", "127.0.0.1"),
		Port:     port,
		Username: getenv("DB_USERNAME", "archio_user"),
		Password: getenv("DB_PASSWORD", "archio_local_password"),
		Database: getenv("DB_DATABASE", "archio"),
	}
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func dsn(cfg config.DBConfig) string {
	u := url.URL{
		Scheme: "postgres",
		User:   url.UserPassword(cfg.Username, cfg.Password),
		Host:   net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port)),
		Path:   "/" + cfg.Database,
	}
	return u.String()
}
