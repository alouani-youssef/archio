package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"sync"
	"time"

	"archio/internal/config"
	"archio/internal/ent"
	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	_ "github.com/jackc/pgx/v5/stdlib"
)

const (
	maxOpenConnections = 25
	maxIdleConnections = 5
)

// Connection owns the PostgreSQL pool and the Ent client built on top of it.
type Connection struct {
	db     *sql.DB
	client *ent.Client

	closeOnce sync.Once
	closeErr  error
}

// Open initializes and verifies a PostgreSQL pool, then shares it with Ent.
func Open(ctx context.Context, cfg config.DBConfig) (*Connection, error) {
	dsn := postgresDSN(cfg)
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("open postgres pool: %w", err)
	}

	db.SetMaxOpenConns(maxOpenConnections)
	db.SetMaxIdleConns(maxIdleConnections)
	db.SetConnMaxIdleTime(5 * time.Minute)
	db.SetConnMaxLifetime(30 * time.Minute)

	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("connect to postgres: %w", err)
	}

	client := ent.NewClient(ent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	return &Connection{db: db, client: client}, nil
}

// Client returns the Ent client that shares this connection's SQL pool.
func (c *Connection) Client() *ent.Client {
	return c.client
}

// Migrate enables pgvector before creating or updating the Ent schema.
func (c *Connection) Migrate(ctx context.Context) error {
	if _, err := c.db.ExecContext(ctx, "CREATE EXTENSION IF NOT EXISTS vector"); err != nil {
		return fmt.Errorf("enable pgvector extension: %w", err)
	}
	if err := c.client.Schema.Create(ctx); err != nil {
		return fmt.Errorf("migrate ent schema: %w", err)
	}
	return nil
}

// Ping reports whether PostgreSQL is reachable within the caller's deadline.
func (c *Connection) Ping(ctx context.Context) error {
	if c == nil || c.db == nil {
		return errors.New("postgres connection is not initialized")
	}
	return c.db.PingContext(ctx)
}

// Close releases the Ent client and its underlying SQL pool once.
func (c *Connection) Close() error {
	if c == nil {
		return nil
	}
	c.closeOnce.Do(func() {
		if c.client != nil {
			c.closeErr = c.client.Close()
			return
		}
		if c.db != nil {
			c.closeErr = c.db.Close()
		}
	})
	return c.closeErr
}

func postgresDSN(cfg config.DBConfig) string {
	dsn := url.URL{
		Scheme: "postgres",
		User:   url.UserPassword(cfg.Username, cfg.Password),
		Host:   net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port)),
		Path:   "/" + cfg.Database,
	}
	return dsn.String()
}
