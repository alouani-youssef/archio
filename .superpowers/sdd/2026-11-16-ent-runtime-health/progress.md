# SDD ledger — plan: docs/superpowers/plans/2026-11-16-ent-runtime-health.md

Ruling: Implement in the existing checkout — the user explicitly requested development here, and moving to a fresh worktree would omit the user's current uncommitted Compose, environment, and design updates.

Pre-flight: Task 1 produces the Ent client and generated entities consumed by Tasks 2 and 3; Task 2 produces the database lifecycle consumed by Task 3. Implement sequentially.

Task 1: complete — added schema.org constants, five Ent schemas, generation directive, dependencies, and generated code; `go generate ./internal/ent` completed.

Task 2: complete — added shared pgx pool, Ent client, pgvector-first migration, Ping, and idempotent Close.

Task 3: complete — added readiness handler, dependency-injected `/health`, application bootstrap, and lifecycle cleanup.

Task 4: complete — documented schema generation, automatic migration, and health behavior; `go build ./cmd/api` succeeded.

Verification: `go generate ./internal/ent` and `go build ./cmd/api` succeeded. `gofmt` and `git diff --check` were clean. `go build ./...` was also attempted and is blocked by pre-existing empty Go files and inconsistent placeholder package declarations outside this feature (cache, events, middlewares, buckets). No tests were added or run, per the developer instruction.
