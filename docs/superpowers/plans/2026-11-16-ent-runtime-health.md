# Ent Runtime and Health Check Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add modular application startup with Ent/PostgreSQL, Redis, and RabbitMQ lifecycles, schema.org core/search entities, and a dependency-backed `/health` readiness endpoint.

**Architecture:** `cmd/api` loads configuration and delegates to `internal/app`; `internal/database` owns the shared pgx SQL pool and Ent client; `internal/cache` owns Redis; `internal/events` owns RabbitMQ; `internal/app` opens, migrates, wires, and closes infrastructure; `internal/modules/health` owns readiness responses; `internal/server` only mounts handlers and runs HTTP.

**Tech Stack:** Go 1.25.6, Ent, pgx/v5, pgvector-go, go-redis, amqp091-go, PostgreSQL 16 with pgvector, Redis, RabbitMQ.

**Spec:** `docs/superpowers/specs/2026-11-16-ent-schemaorg-storage-design.md`

## Global Constraints

- Use the existing `DB_HOST`, `DB_PORT`, `DB_USERNAME`, `DB_PASSWORD`, and `DB_DATABASE` settings.
- Use PostgreSQL and the pgvector extension provided by the current Compose stack.
- Keep process startup, infrastructure lifecycle, feature modules, and HTTP transport separated by responsibility.
- `/health` returns HTTP 503 when PostgreSQL, Redis, or RabbitMQ is not ready and does not expose connection strings, credentials, or raw dependency errors.
- Semantic chunks and exact-search values remain separate from core Bucket/Object/MetaData entities.

## Review Focus

- Infrastructure startup failures: return actionable errors without leaking credentials and close already-opened dependencies.
- Shutdown: close Ent/SQL, Redis, and RabbitMQ connections once after the HTTP server stops.
- Schema migration: create the pgvector extension before Ent creates vector columns/indexes.
- Readiness failures: return 503 with dependency status only, without exposing raw errors.
- Search records: keep exact values indexed and vectors on semantic chunks, not on core objects.

## File Structure

- `internal/ent/schema/*.go`: Ent entity definitions; generated code is emitted to `internal/ent`.
- `internal/schemaorg/terms.go`: commonly used schema.org IRIs.
- `internal/database/connection.go`: PostgreSQL pool, Ent client, migration, readiness ping, and close.
- `internal/cache/connection.go`: Redis client lifecycle and readiness ping.
- `internal/events/broker.go`: RabbitMQ connection lifecycle and readiness check.
- `internal/modules/health/handler.go`: readiness HTTP handler with injected database/cache/broker checkers.
- `internal/app/app.go`: resource initialization, handler wiring, statsviz setup, and lifecycle.
- `internal/server/http.go`: dependency-injected route mounting.
- `cmd/api/main.go`: configuration loading and app invocation.
- `README.md`: generation and run instructions.

### Task 1: Add Ent schemas and schema.org terms

**Files:**
- Create `internal/schemaorg/terms.go`
- Create `internal/ent/generate.go`
- Create `internal/ent/schema/bucket.go`
- Create `internal/ent/schema/object.go`
- Create `internal/ent/schema/metadata.go`
- Create `internal/ent/schema/chunk.go`
- Create `internal/ent/schema/exact_search_value.go`
- Modify `go.mod` and `go.sum`

**Interfaces:**
- `Bucket` maps to schema.org `DataCatalog`; `Object` defaults to `Thing`; `MetaData` identifies `PropertyValue` records.
- `Chunk` has an object edge, ordinal, text, optional pgvector embedding, and HNSW cosine index.
- `ExactSearchValue` has an object edge, property IRI, normalized value, value type, and composite equality index.

- [x] Add Ent, pgx, pgvector-go, Redis, and RabbitMQ dependencies and record Ent CLI as a Go tool.
- [x] Define schema.org IRI constants for common bucket, object, and metadata concepts.
- [x] Define relational entities, edges, uniqueness rules, and the search indexes above.
- [x] Add the Ent `go:generate` directive and generate Ent code.
- [x] Run `gofmt` on generated and handwritten Go files.

### Task 2: Add PostgreSQL and Ent lifecycle management

**Files:**
- Modify `internal/database/connection.go`
- Modify `internal/cache/connection.go`
- Modify `internal/events/broker.go`

**Interfaces:**
- `Open(ctx context.Context, cfg config.DBConfig) (*Connection, error)` opens and pings a shared pgx SQL pool and creates an Ent client from the same pool.
- `(*Connection).Migrate(ctx context.Context) error` enables pgvector before calling Ent schema migration.
- `(*Connection).Ping(ctx context.Context) error` checks PostgreSQL readiness.
- `(*Connection).Close() error` closes the Ent client and underlying pool once.
- `(*Connection).Client() *ent.Client` exposes the Ent client to modules.
- Redis and RabbitMQ each expose `Open(ctx, config)`, `Ping(ctx)`, `Client()`/`Connection()` access, and idempotent `Close()` methods.

- [x] Build a safely escaped PostgreSQL DSN from the current DB configuration.
- [x] Configure bounded pool defaults and ensure startup returns a wrapped error on ping failure.
- [x] Implement extension-first schema migration, ping, client access, and idempotent close.
- [x] Add Redis and RabbitMQ connection packages using the existing config fields and context-aware checks.
- [x] Ensure partially opened connections are closed when a later startup step fails.
- [x] Run `gofmt` on the database package.

### Task 3: Add readiness module and modular application wiring

**Files:**
- Create `internal/modules/health/handler.go`
- Create `internal/app/app.go`
- Modify `internal/server/http.go`
- Modify `cmd/api/main.go`

**Interfaces:**
- Health module depends on database, Redis, and RabbitMQ `Ping(context.Context) error` interfaces and exposes JSON with 200 when all are ready or 503 otherwise.
- `server.NewMux(healthHandler http.Handler) *http.ServeMux` mounts the injected health handler at `/health`.
- `app.New(ctx context.Context, cfg config.Config) (*App, error)`, `(*App).Run(ctx context.Context) error`, and `(*App).Close() error` own startup, server execution, and cleanup.

- [x] Implement dependency-injected readiness handling without exposing raw errors.
- [x] Report PostgreSQL, Redis, and RabbitMQ states from the health handler, without raw errors.
- [x] Build all three connections, run Ent migrations, and register module routes in `app.New`.
- [x] Close every opened connection on startup failure and shutdown.
- [x] Preserve TLS settings, statsviz in development, and graceful HTTP shutdown.
- [x] Replace the inline health route and bootstrap logic in `cmd/api/main.go` with app lifecycle calls.
- [x] Run `gofmt` on changed Go files.

### Task 4: Document setup

**Files:**
- Modify `README.md`

- [x] Document Ent code generation, local Compose startup, automatic schema migration, and `/health` readiness behavior.
- [x] Build the application with `go build ./cmd/api`.

## Verification

- `go generate ./internal/ent` completes and leaves generated code reproducible.
- `go build ./cmd/api` succeeds.
- Do not add or run tests unless requested.
