# Object Storage API Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A MinIO-inspired, API-only object store. It has groups of buckets, API keys per group, versioned object uploads stored on disk, and one authenticated read URL per version.

**Architecture:** Feature modules (`admin`, `buckets`, `objects`) under `internal/modules` follow the existing controller/service/repository split. They are wired by `internal/app` into the `internal/server` mux. Bytes go through an `internal/storage.Storage` interface with a local-disk implementation. Postgres (Ent) is the source of truth for metadata. Uploads publish `object.version.created` to RabbitMQ through `internal/events`.

**Tech Stack:** Go 1.25.6 `net/http` (1.22+ pattern routing), Ent v0.14, pgx/v5, amqp091-go, `github.com/google/uuid`, PostgreSQL 16, RabbitMQ, Docker Compose.

**Spec:** `docs/superpowers/specs/2026-11-17-object-storage-api-design.md`

## Global Constraints

- Own JSON REST API under `/v1`. Not S3-compatible.
- Error body shape: `{"error": {"code": "<snake_case>", "message": "..."}}`.
- Admin endpoints use HTTP Basic auth against `ROOT_USERNAME` / `ROOT_PASSWORD`, compared in constant time. A `401` carries `WWW-Authenticate: Basic`.
- Every other `/v1` endpoint uses `Authorization: Bearer <api-key>`. An unknown or revoked key gets `401`. Anything in another group gets `404`, never `403`.
- API keys: `ak_` followed by base64url (no padding) of 32 random bytes. Only the SHA-256 hex and an 11-character prefix are stored. The plaintext is returned only once, when the key is created.
- Bucket names are unique across the whole instance and match `^[a-z0-9][a-z0-9.-]{1,61}[a-z0-9]$`.
- Object keys: 1–1024 bytes of valid UTF-8. No NUL, no leading `/`, no trailing `/`, no `//`, and no `.` or `..` segments. Unique within a bucket.
- Versions are immutable. A re-upload creates a new version, and old read URLs keep returning the old bytes.
- A DB row must never exist without its bytes on disk. Bytes without a row are tolerated.
- Storage key: `<groupUUID>/<bucketID>/<versionUUID>`. User input never reaches the filesystem.
- Read URL: `GET|HEAD /v1/objects/{versionId}` with headers `ETag: "<sha256>"`, `X-Checksum-Sha256`, `Cache-Control: private, max-age=31536000, immutable`. Supports `If-None-Match` and `Range`.
- Config: `ROOT_USERNAME`, `ROOT_PASSWORD` and `STORAGE_PATH` are required. `PUBLIC_BASE_URL` defaults to `http(s)://APP_HOST:APP_PORT`. `MAX_UPLOAD_BYTES` defaults to `5368709120`.
- Event: durable topic exchange `archio.events`, routing key `object.version.created`. If publishing fails, log it and never fail the upload.
- Integration tests skip when Postgres is unreachable, so `go test ./...` passes without Compose running.
- Commits never mention Claude and carry no co-author trailers.

## Review Focus

- **A client disconnects mid-upload.** No version row is written, nothing is left in `.tmp/`, and the object's current version stays unchanged. *Test in Task 8.*
- **The body exceeds `MAX_UPLOAD_BYTES`.** The response is `413 payload_too_large`, with no row and no file. *Test in Task 8.*
- **Keys with spaces, unicode or percent-encoded characters** (`photos/été 2026/a b.jpg`) round-trip exactly through PUT, the listing and the versions listing. *Test in Task 8.*
- **Two concurrent uploads to the same new key.** Both return `201` with different version IDs, both are readable, and there's one `Object` row. *Test in Task 8.*
- **A key revoked between requests.** The next request, including one to a read URL, gets `401`. *Test in Task 5.*

## File Structure

- `internal/config/config.go`: new `Auth` and `Storage` config sections.
- `pkg/errors/errors.go`: `Error` type and the JSON response writers.
- `internal/storage/storage.go`, `local.go`: the `Storage` interface and the local disk implementation.
- `internal/ent/schema/{group,api_key,object_version}.go` plus edits to `bucket.go` and `object.go`.
- `internal/database/connection.go`: migration options.
- `internal/testutil/db.go`: Postgres test client (skips when unreachable) and unique-name helper.
- `internal/auth/{keys,context}.go`: key generation and hashing, plus the group in the request context.
- `internal/middlewares/authentication.go`: Basic auth and API-key middleware.
- `internal/events/publisher.go`: AMQP publisher and event types.
- `internal/modules/admin/*`: groups and API keys, plus the key authenticator.
- `internal/modules/buckets/*`: bucket CRUD (fills in the existing stubs).
- `internal/modules/objects/*`: key validation, upload, listing, versions, delete and the read URL.
- `internal/server/http.go`, `internal/app/app.go`: wiring.
- `docker-compose.yaml`, `.env.example`, `.gitignore`, `README.md`.

---

### Task 1: Configuration for auth and storage

**Files:**
- Modify: `internal/config/config.go`, `internal/config/config_test.go`, `.env.example`, `.gitignore`

**Interfaces:**
- Produces: `config.Config.Auth config.AuthConfig{RootUsername, RootPassword string}` and `config.Config.Storage config.StorageConfig{Path string; PublicBaseURL string; MaxUploadBytes int64}`.

- [ ] **Step 1: Write failing tests** in `config_test.go`. Add `ROOT_USERNAME=admin`, `ROOT_PASSWORD=secret` and `STORAGE_PATH=/tmp/archio` to `baseValidEnv()`. Then:
  - `TestLoadFrom_StorageDefaults`: `cfg.Storage.MaxUploadBytes == 5368709120` and `cfg.Storage.PublicBaseURL == "http://0.0.0.0:8080"`.
  - `TestLoadFrom_PublicBaseURLWithTLS`: with `TLS_ENABLED=true`, the default starts with `https://`.
  - `TestLoadFrom_PublicBaseURLTrimsSlash`: `PUBLIC_BASE_URL=http://x:1/` becomes `http://x:1`.
  - `TestLoadFrom_RequiresRootAndStorage`: deleting each of `ROOT_USERNAME`, `ROOT_PASSWORD` and `STORAGE_PATH` produces an error mentioning that variable.
  - `TestLoadFrom_InvalidMaxUpload`: `MAX_UPLOAD_BYTES=0` produces an error.
- [ ] **Step 2:** Run `go test ./internal/config/` and expect FAIL (unknown fields).
- [ ] **Step 3: Implement** `loadAuth` and `loadStorage` and their validation in `Validate`. `MAX_UPLOAD_BYTES` must be greater than 0 when set.
- [ ] **Step 4:** Run `go test ./internal/config/` and expect PASS.
- [ ] **Step 5:** Add the five variables to `.env.example` (`STORAGE_PATH=./data/objects`, leave `PUBLIC_BASE_URL` empty) and add `data/` to `.gitignore`.
- [ ] **Step 6: Commit** `feat: add auth and storage configuration`

### Task 2: JSON error and response helpers

**Files:**
- Modify: `pkg/errors/errors.go`
- Test: `pkg/errors/errors_test.go`

**Interfaces:**
- Produces (package `errors`, import as `apperr "archio/pkg/errors"`):
  - `type Error struct { Status int; Code, Message string }` with `Error() string`
  - `func New(status int, code, message string) *Error`
  - `func WriteJSON(w http.ResponseWriter, status int, v any)`: sets `Content-Type: application/json; charset=utf-8`
  - `func Write(w http.ResponseWriter, err error)`: for an `*Error` (via `errors.As`), writes its status and code. Anything else becomes `500` with `internal_error` and the message `"internal server error"`, and the error is logged with `log.Printf`.

- [ ] **Step 1: Write failing tests:**
  - `TestWrite_AppError`: `New(404,"bucket_not_found","bucket not found")` gives status 404 and the body `{"error":{"code":"bucket_not_found","message":"bucket not found"}}`.
  - `TestWrite_UnknownError`: `fmt.Errorf("db password=x")` gives status 500, with code `internal_error`, and the body doesn't contain `password`.
  - `TestWrite_WrappedAppError`: `fmt.Errorf("x: %w", New(409,...))` gives 409.
- [ ] **Step 2:** Run `go test ./pkg/errors/` and expect FAIL.
- [ ] **Step 3: Implement** in `pkg/errors/errors.go`.
- [ ] **Step 4:** Run `go test ./pkg/errors/` and expect PASS.
- [ ] **Step 5: Commit** `feat: add JSON error helpers`

### Task 3: Storage interface and local disk backend

**Files:**
- Create: `internal/storage/storage.go`, `internal/storage/local.go`
- Test: `internal/storage/local_test.go`

**Interfaces:**
- Produces:
  - `var ErrNotFound = errors.New("storage: blob not found")`
  - `type Storage interface { Put(ctx context.Context, storageKey string, r io.Reader) (size int64, sha256Hex string, err error); Open(ctx context.Context, storageKey string) (io.ReadSeekCloser, error); Delete(ctx context.Context, storageKey string) error }`
  - `func Key(groupID uuid.UUID, bucketID int, versionID uuid.UUID) string`, returning `"<group>/<bucket>/<version>"`
  - `func NewLocal(root string) (*Local, error)`: creates `root` and `root/.tmp` (mode 0o750), then probes that it can write by creating and removing a file in `.tmp`.

- [ ] **Step 1: Write failing tests** (use `t.TempDir()`):
  - `TestLocal_PutOpenRoundTrip`: `Put` of `"hello"` returns size 5 and sha `2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824`. `Open` then reads back `"hello"`.
  - `TestLocal_PutEmpty`: an empty reader gives size 0 and sha `e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855`.
  - `TestLocal_PutReaderErrorLeavesNothing`: a reader that returns 3 bytes then `io.ErrUnexpectedEOF` makes `Put` return that error. Afterward, `Open` returns `ErrNotFound` and `root/.tmp` is empty.
  - `TestLocal_PutCanceledContext`: with an already-canceled ctx, `Put` returns `context.Canceled` and `.tmp` is empty.
  - `TestLocal_OpenMissing` and `TestLocal_DeleteMissing`: `Open` returns `ErrNotFound`, and `Delete` returns nil (deleting is idempotent).
  - `TestLocal_RejectsEscapingKey`: `Put(ctx, "../x", r)` returns an error.
  - `TestNewLocal_UnwritableRoot`: a root under a 0o500 directory returns an error.
- [ ] **Step 2:** Run `go test ./internal/storage/` and expect FAIL.
- [ ] **Step 3: Implement** `Local`. `Put` uses `os.CreateTemp(root/.tmp)` and an `io.MultiWriter(file, sha256)` copy that checks `ctx.Err()` between chunks. It then fsyncs the file, `MkdirAll`s the parent, renames, and fsyncs the parent directory. On any error it removes the temp file. Keys are joined with `filepath.Join`, and any key that is absolute, contains `..`, or resolves outside `root` is rejected. Promote `github.com/google/uuid` to a direct dependency (`go mod tidy`).
- [ ] **Step 4:** Run `go test ./internal/storage/` and expect PASS.
- [ ] **Step 5: Commit** `feat: add storage interface and local disk backend`

### Task 4: Ent schema for groups, keys and versions, plus the test DB helper

**Files:**
- Create: `internal/ent/schema/group.go`, `internal/ent/schema/api_key.go`, `internal/ent/schema/object_version.go`, `internal/testutil/db.go`
- Modify: `internal/ent/schema/bucket.go`, `internal/ent/schema/object.go`, `internal/database/connection.go`, then regenerate `internal/ent/`
- Test: `internal/database/schema_test.go`

**Interfaces:**
- Produces (Ent):
  - `Group`: `id` UUID (default `uuid.New`), `name` string NotEmpty Unique, `created_at`, `updated_at`. Edges: `buckets` to Bucket, `api_keys` to ApiKey.
  - `ApiKey`: `id` UUID, `name` NotEmpty, `prefix` NotEmpty, `key_hash` NotEmpty Unique Sensitive, `created_at`, `last_used_at` Optional Nillable, `revoked_at` Optional Nillable. Edge `group`: From Group `api_keys`, Unique Required.
  - `ObjectVersion`: `id` UUID, `size` int64 NonNegative, `checksum_sha256` NotEmpty, `content_type` NotEmpty, `storage_key` NotEmpty, `created_at` Immutable. Edge `object`: From Object `versions`, Unique Required. Index on the `object` edge plus `created_at`.
  - `Bucket`: `name` gets Unique (instance-wide). New edge `group`: From Group `buckets`, Unique Required.
  - `Object`: remove the `url` field. Add edges `versions` (To ObjectVersion) and `current_version` (To ObjectVersion, Unique).
  - `Connection.Migrate` passes `migrate.WithDropColumn(true), migrate.WithDropIndex(true)` to `Schema.Create`.
  - `testutil.OpenDB(t *testing.T) *ent.Client`: reads `DB_*` from the env (falling back to the `.env.example` values), opens with `database.Open`, calls `t.Skip` if the ping fails within 2s, runs `Migrate`, and closes on `t.Cleanup`.
  - `testutil.Unique(prefix string) string`: returns `prefix + "-" + 8 random lowercase hex characters` (always a valid bucket name).

- [ ] **Step 1: Write a failing test** `TestSchema_GroupBucketObjectVersion` (in `internal/database`, using `testutil.OpenDB`). It creates a Group, a Bucket in it, an Object, and two ObjectVersions, then sets `current_version` to the second. It asserts that `QueryVersions().Count()==2` and that `QueryCurrentVersion().OnlyID()` is the second ID. It also asserts that creating a second bucket with the same name in **another** group fails with `ent.IsConstraintError`.
- [ ] **Step 2:** Run `go test ./internal/database/` and expect a build FAIL (missing types).
- [ ] **Step 3: Implement** the schemas, then run `go generate ./internal/ent`.
- [ ] **Step 4:** Run `docker compose down -v && docker compose up -d postgres redis rabbitmq`, then `go test ./internal/database/`. Expect PASS. Without Compose running it should SKIP.
- [ ] **Step 5:** Add to `README.md` that this schema change needs a fresh volume (`docker compose down -v`).
- [ ] **Step 6: Commit** `feat: add group, api key and object version schemas`

### Task 5: API keys, auth middleware and admin module

**Files:**
- Create: `internal/auth/keys.go`, `internal/auth/context.go`, `internal/modules/admin/{controller,dto,repository,service,route_v1}.go`
- Modify: `internal/middlewares/authentication.go`
- Test: `internal/auth/keys_test.go`, `internal/middlewares/authentication_test.go`, `internal/modules/admin/admin_test.go`

**Interfaces:**
- Consumes: `apperr.*` (Task 2), Ent `Group` and `ApiKey` (Task 4), `testutil.*` (Task 4).
- Produces:
  - `auth.GenerateKey() (plaintext, prefix, hash string, err error)` and `auth.HashKey(plaintext string) string`
  - `auth.WithGroup(ctx context.Context, groupID uuid.UUID) context.Context` and `auth.GroupFrom(ctx context.Context) (uuid.UUID, bool)`
  - `middlewares.BasicAuth(username, password string) func(http.Handler) http.Handler`
  - `type middlewares.KeyAuthenticator interface { Authenticate(ctx context.Context, keyHash string) (uuid.UUID, error) }`. A returned `*apperr.Error` with status 401 means the key was rejected.
  - `middlewares.APIKeyAuth(a KeyAuthenticator) func(http.Handler) http.Handler`
  - `admin.NewKeyAuthenticator(client *ent.Client) middlewares.KeyAuthenticator`: looks the hash up among keys with `revoked_at IS NULL` and returns the group ID. It updates `last_used_at` with a single `UPDATE ... WHERE id=? AND (last_used_at IS NULL OR last_used_at < now()-1 minute)`.
  - `admin.RegisterRoutes(mux *http.ServeMux, client *ent.Client, basic func(http.Handler) http.Handler)` mounts the six admin routes from the spec.
  - JSON: group `{id,name,createdAt}`. Key `{id,name,prefix,createdAt,lastUsedAt,revokedAt}`, plus `key` (plaintext) only in the `201` create response.

- [ ] **Step 1: Write failing tests:**
  - `TestGenerateKey`: plaintext matches `^ak_[A-Za-z0-9_-]{43}$`, `prefix == plaintext[:11]`, `hash == HashKey(plaintext)` (64 hex characters), and two calls differ.
  - `TestBasicAuth`: no header, a wrong user, or a wrong password each give 401 with `WWW-Authenticate: Basic realm="archio"`. The correct credentials reach the next handler.
  - `TestAPIKeyAuth`: a missing or non-`Bearer` header gives 401 with code `unauthorized`. A fake authenticator that returns group G leads to a next handler where `auth.GroupFrom` returns G.
  - `TestAdmin_GroupAndKeyLifecycle` (DB, through `httptest`): `POST /v1/admin/groups {name}` returns 201, and the same name again returns 409 `group_exists`. `POST .../keys {name}` returns 201 with `key`. `GET .../keys` returns no `key` and no hash field. `NewKeyAuthenticator.Authenticate(HashKey(key))` returns the group. **Review Focus:** after `DELETE .../keys/{id}` (204), `Authenticate` returns a 401 error.
  - `TestAdmin_DeleteGroupWithBuckets`: a group with one bucket gives 409 `group_not_empty`. An empty group gives 204, and its keys are gone.
  - `TestAdmin_UnknownGroup`: keys routes on a random UUID give 404 `group_not_found`. A malformed UUID also gives 404.
- [ ] **Step 2:** Run `go test ./internal/auth/ ./internal/middlewares/ ./internal/modules/admin/` and expect FAIL.
- [ ] **Step 3: Implement.** Compare credentials with `crypto/subtle.ConstantTimeCompare` on SHA-256 digests of both values, so a length mismatch doesn't leak timing.
- [ ] **Step 4:** Run the same command and expect PASS.
- [ ] **Step 5: Commit** `feat: add api keys, auth middleware and admin endpoints`

### Task 6: Event publisher

**Files:**
- Modify: `internal/events/publisher.go`
- Test: `internal/events/publisher_test.go`

**Interfaces:**
- Produces:
  - `const Exchange = "archio.events"` and `const ObjectVersionCreated = "object.version.created"`
  - `type Publisher interface { Publish(ctx context.Context, routingKey string, payload any) error }`
  - `type ObjectVersionCreatedEvent struct { VersionID uuid.UUID "versionId"; ObjectKey string "objectKey"; BucketID int "bucketId"; BucketName string "bucketName"; GroupID uuid.UUID "groupId"; Size int64 "size"; ChecksumSHA256 string "checksumSha256"; ContentType string "contentType"; CreatedAt time.Time "createdAt" }` (the quoted names are the JSON tags)
  - `func NewPublisher(b *Broker) (*AMQPPublisher, error)`: opens a dedicated channel and declares the exchange `topic`, durable.
  - `(*AMQPPublisher) Close() error`
  - `type NoopPublisher struct{}` for tests.

- [ ] **Step 1: Write failing tests:** `TestObjectVersionCreatedEvent_JSON` asserts the exact camelCase keys above. `TestAMQPPublisher_Publish` connects using `.env.example` broker values and calls `t.Skip` if `Open` fails. It publishes to a temporary queue bound to `object.version.created` and receives the same JSON.
- [ ] **Step 2:** Run `go test ./internal/events/` and expect FAIL.
- [ ] **Step 3: Implement.** Serialize `PublishWithContext` with a mutex (AMQP channels aren't goroutine-safe), use `ContentType: application/json` and `DeliveryMode: amqp.Persistent`.
- [ ] **Step 4:** Run `go test ./internal/events/` and expect PASS (or SKIP for the broker test).
- [ ] **Step 5: Commit** `feat: add event publisher for object version events`

### Task 7: Buckets module

**Files:**
- Modify: `internal/modules/buckets/{controller,dto,entity,repository,service,route_v1}.go` (rename the empty stub packages to `package buckets`)
- Test: `internal/modules/buckets/buckets_test.go`

**Interfaces:**
- Consumes: `auth.GroupFrom`, `apperr.*`, `testutil.*`, Ent.
- Produces:
  - `buckets.ValidateName(name string) error`
  - `buckets.Resolve(ctx context.Context, client *ent.Client, groupID uuid.UUID, name string) (*ent.Bucket, error)`: returns `apperr` 404 `bucket_not_found` when the bucket doesn't exist or belongs to another group. Task 8 uses it.
  - `buckets.RegisterRoutes(mux *http.ServeMux, client *ent.Client, keyAuth func(http.Handler) http.Handler)`
  - JSON bucket: `{name, description, createdAt}`. The list response is `{buckets: [...]}`, sorted by name.

- [ ] **Step 1: Write failing tests:**
  - `TestValidateName`: `abc`, `my.bucket-1` and a 63-character name are valid. `ab`, a 64-character name, `Abc`, `-abc`, `abc-` and `a_b` are invalid.
  - `TestBuckets_CRUD` (DB, `httptest`, with a context-injecting fake auth middleware for group A): create returns 201, `GET` returns it, the list contains it, and `DELETE` gives 204, after which `GET` gives 404.
  - `TestBuckets_NameUniqueAcrossGroups`: group B creating the same name gets 409 `bucket_exists`.
  - `TestBuckets_CrossGroupIs404`: group B doing `GET` or `DELETE` on group A's bucket gets 404 `bucket_not_found`. B's list doesn't include it.
  - `TestBuckets_DeleteNonEmpty`: a bucket with one Object row gives 409 `bucket_not_empty`.
  - `TestBuckets_InvalidName`: 400 `invalid_bucket_name`.
- [ ] **Step 2:** Run `go test ./internal/modules/buckets/` and expect FAIL.
- [ ] **Step 3: Implement.** Map `ent.IsConstraintError` on create to 409.
- [ ] **Step 4:** Run `go test ./internal/modules/buckets/` and expect PASS.
- [ ] **Step 5: Commit** `feat: add bucket endpoints`

### Task 8: Objects module (upload, listing, versions, delete)

**Files:**
- Create: `internal/modules/objects/{keys,dto,repository,service,controller,route_v1}.go`
- Test: `internal/modules/objects/keys_test.go`, `internal/modules/objects/objects_test.go`

**Interfaces:**
- Consumes: `storage.Storage` and `storage.Key` (Task 3), `buckets.Resolve` (Task 7), `events.Publisher` (Task 6), `auth.GroupFrom`, `apperr.*`, `testutil.*`.
- Produces:
  - `objects.ValidateKey(key string) error`
  - `type objects.Deps struct { Client *ent.Client; Storage storage.Storage; Publisher events.Publisher; PublicBaseURL string; MaxUploadBytes int64 }`
  - `objects.RegisterRoutes(mux *http.ServeMux, d Deps, keyAuth func(http.Handler) http.Handler)` mounts:
    - `PUT /v1/buckets/{bucket}/objects/{key...}`
    - `GET /v1/buckets/{bucket}/objects`
    - `GET /v1/buckets/{bucket}/versions/{key...}`
    - `DELETE /v1/buckets/{bucket}/objects/{key...}`
    - plus the read route from Task 9
  - JSON version: `{versionId, key, size, checksumSha256, contentType, createdAt, readUrl}`, where `readUrl = PublicBaseURL + "/v1/objects/" + versionId`.
  - JSON object: `{key, updatedAt, currentVersion: <version>}`. The list is `{objects: [...], nextCursor?: string}`. The versions list is `{versions: [...]}`, newest first.

- [ ] **Step 1: Write failing tests:**
  - `TestValidateKey`: `a`, `photos/été 2026/a b.jpg` and a 1024-byte key are valid. Invalid: an empty key, 1025 bytes, `"\x00"`, invalid UTF-8 `"\xff"`, `/a`, `a/`, `a//b`, `a/./b` and `a/../b`.
  - `TestUpload_CreatesVersion` (DB, `t.TempDir()` storage, fake publisher): `PUT` with body `hello` and `Content-Type: text/plain` returns 201 with size 5, the sha of `hello`, `text/plain` and `readUrl` ending in `/v1/objects/<versionId>`. The file exists at `storage.Key(...)`. The fake publisher received one `object.version.created` event with matching fields.
  - `TestUpload_DefaultContentTypeAndEmptyBody`: no content type gives `application/octet-stream`. An empty body gives size 0.
  - `TestUpload_ReuploadAddsVersion`: two PUTs to the same key produce two distinct `versionId`s. `GET .../versions/{key}` lists both, newest first, and the listing's `currentVersion` is the second.
  - **Review Focus:** `TestUpload_UnicodeKeyRoundTrip`: `PUT /v1/buckets/b/objects/photos/%C3%A9t%C3%A9%202026/a%20b.jpg` gives a response `key` of `photos/été 2026/a b.jpg`. The listing and the versions route return the same key.
  - **Review Focus:** `TestUpload_TooLarge`: with `MaxUploadBytes=4` and a 5-byte body, the response is 413 `payload_too_large`. Afterward there's no Object row and the storage `.tmp` is empty.
  - **Review Focus:** `TestUpload_ClientAbort`: the request body is a reader that errors after 3 bytes. There's no version row, `.tmp` is empty, and the previously current version is unchanged.
  - **Review Focus:** `TestUpload_ConcurrentSameNewKey`: 2 goroutines PUT to the same new key. Both get 201 with different IDs, there's exactly 1 Object row, and there are 2 versions.
  - `TestUpload_PublishFailureStillSucceeds`: a publisher that returns an error still gives 201.
  - `TestUpload_InvalidKey` gives 400 `invalid_object_key` (send `a/./b` via a raw `RequestURI`, since `httptest.NewRequest` cleans paths). `TestUpload_UnknownBucket` and `TestUpload_OtherGroupBucket` both give 404 `bucket_not_found`.
  - `TestList_PrefixAndPagination`: keys `a/1`, `a/2`, `a/3` and `b/1` with `prefix=a/&limit=2` give 2 items plus a `nextCursor`. The next page returns `a/3` and no `nextCursor`. A `limit` of 0 or over 1000 gives 400 `invalid_limit`.
  - `TestDelete_RemovesVersionsAndFiles`: after `DELETE`, the response is 204, a `GET` on versions gives 404 `object_not_found`, both files are gone, and a MetaData row attached beforehand is gone too.
- [ ] **Step 2:** Run `go test ./internal/modules/objects/` and expect FAIL.
- [ ] **Step 3: Implement `ValidateKey`** in `keys.go`.
- [ ] **Step 4: Implement upload** in `service.go`:
  1. `buckets.Resolve` and `ValidateKey`.
  2. Call `http.NewResponseController(w).SetReadDeadline(time.Now().Add(time.Hour))`, and the same for the write deadline.
  3. `r.Body = http.MaxBytesReader(w, r.Body, MaxUploadBytes)`.
  4. Generate `versionID := uuid.New()` and call `Storage.Put`. A `*http.MaxBytesError` maps to 413.
  5. Run the DB transaction: get the Object by (bucket, identifier) or create it, create the ObjectVersion, and set `current_version`. If the create hits `ent.IsConstraintError`, roll back and retry the whole transaction once. That handles the concurrent-new-key race.
  6. If the transaction fails, call `Storage.Delete(storageKey)`.
  7. Publish the event with a 2s timeout, logging any failure.
- [ ] **Step 5: Implement listing** (cursor = base64url of the last returned key, query `identifier > cursor ORDER BY identifier LIMIT limit+1`), **versions** (`ORDER BY created_at DESC, id`) and **delete**. Delete runs in one transaction: clear `current_version`, delete the versions, MetaData, Chunk and ExactSearchValue rows, then the object. After commit, call `Storage.Delete` for each version and log any failure.
- [ ] **Step 6:** Run `go test ./internal/modules/objects/` and expect PASS.
- [ ] **Step 7: Commit** `feat: add object upload, listing, versions and delete`

### Task 9: Read URL endpoint

**Files:**
- Create: `internal/modules/objects/read.go`
- Modify: `internal/modules/objects/route_v1.go`
- Test: `internal/modules/objects/read_test.go`

**Interfaces:**
- Consumes: `Deps` (Task 8), `storage.ErrNotFound`.
- Produces: `GET /v1/objects/{versionId}`, which Go's mux also serves for `HEAD`.

- [ ] **Step 1: Write failing tests:**
  - `TestRead_ReturnsBytesAndHeaders`: the response is 200 with body `hello`, `Content-Type: text/plain`, `Content-Length: 5`, `ETag: "<sha>"`, `X-Checksum-Sha256: <sha>` and `Cache-Control: private, max-age=31536000, immutable`.
  - `TestRead_OldVersionStillServed`: after a re-upload with `world`, the first version's URL still returns `hello`.
  - `TestRead_Head`: same headers, empty body.
  - `TestRead_IfNoneMatch`: a matching ETag gives 304.
  - `TestRead_Range`: `Range: bytes=1-3` gives 206 with body `ell`.
  - `TestRead_OtherGroupIs404`, `TestRead_UnknownVersionIs404` and `TestRead_MalformedIDIs404` all give 404 `version_not_found`.
  - `TestRead_MissingFileIs500`: delete the file behind the API's back. The response is 500 `internal_error`, and the error is logged.
- [ ] **Step 2:** Run `go test ./internal/modules/objects/ -run TestRead` and expect FAIL.
- [ ] **Step 3: Implement.** Load the version together with its object, bucket and group, and compare the group ID to `auth.GroupFrom`. Extend the write deadline by 1h, set the headers, and call `http.ServeContent(w, r, "", version.CreatedAt, file)`.
- [ ] **Step 4:** Run `go test ./internal/modules/objects/` and expect PASS.
- [ ] **Step 5: Commit** `feat: add per-version read url endpoint`

### Task 10: App wiring, Compose and docs

**Files:**
- Modify: `internal/server/http.go`, `internal/app/app.go`, `docker-compose.yaml`, `README.md`
- Test: `internal/app/app_test.go`

**Interfaces:**
- Consumes: everything above.
- Produces: `server.NewMux(healthHandler http.Handler, registrars ...func(*http.ServeMux)) *http.ServeMux`.

- [ ] **Step 1: Write a failing end-to-end test** `TestApp_EndToEnd`. It skips if Postgres or RabbitMQ is unreachable. It builds `app.New` with a temp `STORAGE_PATH` and serves `App.Handler()` through `httptest.NewServer`. The flow:
  1. Admin creates a group and a key.
  2. Create a bucket.
  3. PUT `doc.txt` twice, with `v1` then `v2`.
  4. Each `readUrl` returns its own bytes.
  5. No key gives 401.
  6. A second group's key gives 404 on both read URLs.
  7. `/health` gives 200.

  Expose `func (a *App) Handler() http.Handler` for this.
- [ ] **Step 2:** Run `go test ./internal/app/` and expect FAIL.
- [ ] **Step 3: Implement the wiring.** In `app.New`, call `storage.NewLocal(cfg.Storage.Path)` and `events.NewPublisher(broker)`. If either fails, close what's already open, matching the existing pattern. Register admin (wrapped in `BasicAuth`), buckets and objects (wrapped in `APIKeyAuth(admin.NewKeyAuthenticator(client))`). Close the publisher in `App.Close` before the broker.
- [ ] **Step 4:** Run `go test ./internal/app/` and expect PASS.
- [ ] **Step 5: Update Compose.** Add the volume `object_data`. Add a service `api`: `build: .`, `ports: ["127.0.0.1:${APP_PORT:-3000}:3000"]`, `env_file: .env`, and `environment` overrides `APP_HOST=0.0.0.0`, `APP_PORT=3000`, `DB_HOST=postgres`, `REDIS_HOST=redis`, `BROKER_HOST=rabbitmq`, `STORAGE_PATH=/data/objects`, `PUBLIC_BASE_URL=http://127.0.0.1:${APP_PORT:-3000}`. Mount `object_data:/data/objects`, and set `depends_on` with `condition: service_healthy` for all three dependencies. In the `Dockerfile`, create `/data/objects` and make it writable.
- [ ] **Step 6:** Run `docker compose up -d --build` and verify that `curl -s 127.0.0.1:3000/health` returns `"status":"ok"`. Then run a manual curl flow: create a group and key, create a bucket, PUT, and GET the `readUrl`.
- [ ] **Step 7: Update the README** with the API overview (the endpoint table), the new environment variables, a curl quickstart and the reset note.
- [ ] **Step 8:** Run `go vet ./... && go test ./...`. Expect PASS with no failures (SKIPs are allowed without Compose).
- [ ] **Step 9: Commit** `feat: wire object storage api and add compose api service`
