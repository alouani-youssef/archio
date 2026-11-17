# Object Storage API Design

## Intent

Archio takes inspiration from MinIO for its core storage features: creating buckets and uploading objects into them. Unlike MinIO, Archio has **no visual interface**; everything happens through an HTTP API. Every **version** of an object has exactly **one stable read URL**.

This is the storage foundation for Archio's later schema.org metadata and search features (`MetaData`, `Chunk`, `ExactSearchValue`, which already exist in the Ent schema). This work builds the storage core and emits an event they can attach to later.

## Goals

- Groups of buckets, with access controlled by API keys scoped to a group.
- Admin endpoints protected by root credentials, for managing groups and API keys.
- Bucket create, list, get and delete within a group.
- Object upload, where each upload creates a new immutable version that records its size, checksum, content type and storage location.
- Object listing, version listing and object deletion.
- One public read URL per object version.
- File bytes stored on a disk volume behind a storage interface, with a local-disk implementation.
- An `object.version.created` event published to RabbitMQ after each successful upload.

## Non-goals

- S3 protocol compatibility (SigV4, XML responses, S3 SDK/`mc` support). Archio exposes its own JSON REST API. S3 compatibility may come later.
- A web console or any visual interface.
- Presigned or expiring URLs, and per-version access control on read URLs.
- Multipart/chunked uploads, resumable uploads, or range requests on upload.
- Content deduplication.
- Consumers of the `object.version.created` event, which belong to the later metadata and search work.
- Read-only or otherwise scoped API keys. Every key has full access to its group.
- Multiple admin users. There is one root admin.
- Non-local storage backends (S3, MinIO). The interface allows them, but none is built now.

## Anti-goals

- A version's bytes must never change after its read URL is issued. Re-uploading creates a new version and never overwrites an earlier one.
- The database must never point to a version whose bytes are missing or incomplete.
- A key from one group must never be able to list, modify or detect objects, versions or buckets of another group through the key-protected API. (Read URLs are public by design.)
- Plaintext API keys must never be stored or logged.

## Approaches

1. **One file per version behind a storage interface (chosen).** Each version is its own file on disk, accessed through a small `Storage` interface. Uploads stream to a temp file while the size and checksum are computed, the file is moved into place, and then the DB row is committed. Identical content is stored twice, but deleting a version means deleting its file, and other backends can be added without changing the API.
2. **Content-addressed storage by checksum.** Files are named by SHA-256, so identical content is stored once. It saves disk, but deletion needs reference counting, and a bug there loses data. Rejected because deduplication isn't needed and the added risk isn't worth it.
3. **Write first, process asynchronously.** The upload returns as soon as the bytes are written, and a RabbitMQ worker computes the checksum and marks the version ready. The upload response could not include the checksum, and versions would not be readable until processed. Rejected because it complicates the upload-then-read flow.

The user chose approach 1.

## Decisions made with the user

- Own JSON REST API, not S3-compatible.
- Objects have versions, and each version has its size, checksum, content type and storage location.
- The bytes live on a disk volume added to Docker Compose.
- Read URLs are public (changed 2026-11-17 at the user's request; originally they required an API key). Anyone holding a read URL can download that version; version IDs are random UUIDs, so URLs can't be guessed.
- API keys are issued per **group of buckets**. A key unlocks every bucket in its group.
- Admin endpoints (groups and API keys) are protected by `ROOT_USERNAME` / `ROOT_PASSWORD` over HTTP Basic auth.
- `ARCHIO_ROOT_KEY` is dropped. There's no bypass key.
- Bucket names are unique across the whole instance, as in MinIO.
- New settings: `STORAGE_PATH`, `PUBLIC_BASE_URL`, `MAX_UPLOAD_BYTES`.

## Data model (Ent)

New entities:

- **`Group`**: `id` (UUID), `name` (unique, non-empty), `created_at`, `updated_at`. Has many `Bucket` and many `ApiKey`.
- **`ApiKey`**: `id` (UUID), `name` (label), `prefix` (the first characters of the key, for identification in listings), `key_hash` (SHA-256 hex, unique), `created_at`, `last_used_at` (optional), `revoked_at` (optional). Belongs to one `Group`.
- **`ObjectVersion`**: `id` (UUID v4, which is also the version ID in the read URL), `size` (int64), `checksum_sha256` (hex), `content_type`, `storage_key` (the location inside the storage backend), `created_at`. Belongs to one `Object`. Immutable once created.

Changes to existing entities:

- **`Bucket`**: belongs to one `Group` (required). `name` is unique **across the whole Archio instance**, as in MinIO (unique index on `name`). Bucket name rules: 3–63 characters, lowercase letters, digits, `-` and `.`, starting and ending with a letter or digit. These are MinIO/S3-style names, so a bucket name is always safe to use as a path segment.
- **`Object`**: `identifier` is the object key, unique within its bucket (as it is today). Add a `current_version` edge (optional, unique) pointing to its newest `ObjectVersion`. The existing `url` field is superseded by per-version read URLs and should be removed.

Object key rules: 1–1024 bytes of UTF-8, no NUL bytes, and no leading `/`. Keys may contain `/` to express "folders", as in MinIO. Keys are never used as filesystem paths (see Storage).

## Authentication

- **Admin endpoints** (`/v1/admin/...`) require HTTP Basic auth, compared in constant time against `ROOT_USERNAME` and `ROOT_PASSWORD`. Missing or wrong credentials return `401` with `WWW-Authenticate: Basic`.
- **All other `/v1` endpoints** require `Authorization: Bearer <api-key>`. The server hashes the presented key with SHA-256 and looks it up by `key_hash`. If the key isn't found or has been revoked, the response is `401`. Otherwise the request carries the key's group in its context.
- **Key format:** `ak_` followed by 32 random bytes, base64url-encoded. The full key is returned exactly once, in the creation response. Only its SHA-256 hash and a short prefix are stored. A plain SHA-256 is sufficient because keys are high-entropy random values, not passwords.
- **Cross-group access** returns `404`, never `403`, so callers can't learn that a bucket, object or version exists in another group.
- `last_used_at` may be updated at most about once a minute per key, to avoid a write on every request. *(Builder's call how.)*
- Config: `ROOT_USERNAME` and `ROOT_PASSWORD` are required. Startup fails if either is empty, which matches how the config already handles missing DB, Redis and broker settings.

## HTTP API

All responses are JSON except object downloads. Errors use one shape: `{"error": {"code": "bucket_not_found", "message": "..."}}`.

### Admin (Basic auth)

| Method & path | Behavior |
|---|---|
| `POST /v1/admin/groups` | Create a group `{name}`. Returns `409` if the name is taken. |
| `GET /v1/admin/groups` | List groups. |
| `DELETE /v1/admin/groups/{groupId}` | Delete a group. Returns `409` if it still has buckets. Its keys are deleted with it. |
| `POST /v1/admin/groups/{groupId}/keys` | Issue a key `{name}`. The response includes the plaintext key, shown only this once. |
| `GET /v1/admin/groups/{groupId}/keys` | List keys (id, name, prefix, created, last used, revoked). Never returns hashes or plaintext. |
| `DELETE /v1/admin/groups/{groupId}/keys/{keyId}` | Revoke the key by setting `revoked_at`. The key stops working immediately. |

### Buckets (API key, scoped to the key's group)

| Method & path | Behavior |
|---|---|
| `POST /v1/buckets` | Create `{name, description?}`. Returns `409` if the name exists anywhere in the instance (in any group) and `400` if the name is invalid. The `409` is the one accepted exception to cross-group non-disclosure: as in MinIO, a taken bucket name is visible to anyone trying to create it. |
| `GET /v1/buckets` | List the group's buckets. |
| `GET /v1/buckets/{bucket}` | Bucket details. |
| `DELETE /v1/buckets/{bucket}` | Delete the bucket. Returns `409` unless it's empty (no objects). |

### Objects (API key)

| Method & path | Behavior |
|---|---|
| `PUT /v1/buckets/{bucket}/objects/{key...}` | Upload the raw request body as a new version. `Content-Type` is stored, and defaults to `application/octet-stream`. Returns `201` with the version: `{versionId, key, size, checksumSha256, contentType, createdAt, readUrl}`. Creates the `Object` if the key is new. |
| `GET /v1/buckets/{bucket}/objects?prefix=&limit=&cursor=` | List objects with their current version, sorted by key and paginated by cursor. `limit` defaults to 100, with a maximum of 1000. |
| `GET /v1/buckets/{bucket}/versions/{key...}` | List all versions of the object, newest first, each with its `readUrl`. |
| `DELETE /v1/buckets/{bucket}/objects/{key...}` | Delete the object and all its versions, including their files. |

Object keys that contain `/` use Go 1.22+ `{key...}` wildcard routing. The versions listing lives under its own `/versions/` prefix, so a key that ends in `/versions` stays unambiguous.

### Read URL (public)

| Method & path | Behavior |
|---|---|
| `GET /v1/objects/{versionId}` | Streams the version's bytes. No credentials; an unknown or malformed version ID returns `404`. |
| `HEAD /v1/objects/{versionId}` | The same headers, without the body. |

Response headers: `Content-Type`, `Content-Length`, `ETag: "<sha256>"`, `Cache-Control: public, max-age=31536000, immutable`, `X-Checksum-Sha256`. `If-None-Match` returns `304`. Range requests (`Range` header) should be supported by serving the file through `http.ServeContent`.

`readUrl` in responses is an absolute URL built from a new `PUBLIC_BASE_URL` setting, for example `http://127.0.0.1:3000`. It defaults to `http://APP_HOST:APP_PORT`, or `https` when TLS is enabled.

## Storage

```go
type Storage interface {
    // Put streams r into a new blob and returns its size and SHA-256.
    // The blob must not be visible at storageKey until it is completely written.
    Put(ctx context.Context, storageKey string, r io.Reader) (size int64, sha256Hex string, err error)
    Open(ctx context.Context, storageKey string) (io.ReadSeekCloser, error)
    Delete(ctx context.Context, storageKey string) error
}
```

- **Local disk implementation**, rooted at `STORAGE_PATH`. The `storage_key` is `<groupId>/<bucketId>/<versionId>`, built entirely from server-generated UUIDs. User-supplied names and keys never touch the filesystem, which rules out path traversal.
- `Put` writes to a temp file in `STORAGE_PATH/.tmp/`, which is on the same filesystem so the rename is atomic. It hashes and counts bytes while writing, then `fsync`s, renames into place and `fsync`s the directory.
- `STORAGE_PATH` is required. At startup the server creates it if it's missing and fails if it isn't writable.

## Upload flow

1. Authenticate, then resolve the bucket in the key's group (`404` if not found).
2. Validate the key. Enforce `MAX_UPLOAD_BYTES` (a new setting, default 5 GiB) with `http.MaxBytesReader`. If the limit is exceeded, return `413` and delete the temp file.
3. Generate the version UUID, then `Storage.Put` the body.
4. In one DB transaction, upsert the `Object` by (bucket, key), create the `ObjectVersion`, and set it as `current_version`. Concurrent uploads to the same key must both succeed and create two versions. The newest committed version becomes current.
5. If the transaction fails, delete the stored file. *(A file without a DB row is harmless garbage. A DB row without a file is never allowed.)*
6. After commit, publish `object.version.created` (see Events). A publish failure is logged and does not fail the upload.

## Delete flow

- **Object delete:** in one transaction, delete the object and its version rows, then delete the files. If deleting a file fails afterward, log it. The orphaned file is harmless.
- **Bucket delete:** allowed only when the bucket is empty.
- An orphaned-file sweeper is out of scope and can be added later.

## Events

- Declare a durable topic exchange `archio.events`.
- After each successful upload, publish routing key `object.version.created` with a JSON body: `{versionId, objectKey, bucketId, bucketName, groupId, size, checksumSha256, contentType, createdAt}`.
- No consumer is built now.

## HTTP server timeouts

The current server has `ReadTimeout` 15s and `WriteTimeout` 30s, which would cut off large uploads and downloads. Upload and read handlers must extend their own deadlines (for example with `http.ResponseController.SetReadDeadline` / `SetWriteDeadline`) or the server-wide timeouts must be adjusted. `ReadHeaderTimeout` stays in place to guard against slowloris. *(Builder's call which mechanism.)*

## Configuration and Compose

New settings in `.env.example` and config validation:

- `ROOT_USERNAME`, `ROOT_PASSWORD` (required)
- `STORAGE_PATH` (required; for example `./data/objects` when running on the host)
- `PUBLIC_BASE_URL` (optional, defaulted as above)
- `MAX_UPLOAD_BYTES` (optional, default `5368709120`)

`docker-compose.yaml`: add a named volume `object_data`. Today the API runs on the host (`go run ./cmd/api`) and Compose only runs its dependencies. Add an `api` service built from the existing `Dockerfile`. It mounts `object_data` at `/data/objects`, sets `STORAGE_PATH=/data/objects`, points the DB, Redis and broker hosts at their Compose service names, and waits for their health checks. Add `data/` to `.gitignore` for host runs.

## Code layout

Follow the existing module pattern (`internal/modules/<name>/` with controller, dto, repository, service and routes):

- `internal/modules/admin`: groups and API keys
- `internal/modules/buckets`: currently empty stubs, to be filled in
- `internal/modules/objects`: upload, listing, versions, delete and the read URL
- `internal/storage`: the `Storage` interface and the local disk implementation
- `internal/middlewares/authentication.go`: Basic auth for admin and API-key auth for everything else
- `pkg/errors`: the shared error type and the JSON error writer
- `internal/server/http.go` / `internal/app/app.go`: wire the new modules and storage into the mux and the app lifecycle

## Testing

- Unit tests: name and key validation, key generation and hashing, and the local storage `Put`/`Open`/`Delete`, including temp-file cleanup on failure and on exceeding the size limit.
- Integration tests against the Compose Postgres: group isolation (a key from group A gets `404` on group B's bucket and objects), revoked keys get `401`, read URLs work without credentials, re-uploads create new versions while old read URLs still return the old bytes, concurrent uploads to one key, and the delete flows.

## Settled after review (designer's calls)

- **Existing local data is disposable.** No API writes buckets or objects yet, so making the bucket's group required and dropping `Object.url` doesn't need a data migration. Enable Ent's `WithDropColumn` and `WithDropIndex` for auto-migrate, and document "run `docker compose down -v` to reset" in the README.
- **Keys Go's router would rewrite are rejected.** Object keys containing empty segments (`//`), `.` or `..` segments, or ending with `/` are rejected with `400`. Otherwise Go's `ServeMux` would clean or redirect them. This adds to the key rules above.
- **Object delete includes related rows.** An object's `MetaData`, `Chunk` and `ExactSearchValue` rows are deleted in the same transaction as the object and its versions.
- **Integration tests skip without Postgres.** They skip when the database can't be reached, so `go test ./...` passes without Compose running.
- **The Compose `api` service sets `PUBLIC_BASE_URL` explicitly** (for example `http://127.0.0.1:3000`) and `APP_HOST=0.0.0.0`. The default derived from `APP_HOST` would produce broken read URLs there.

## Left to the builder

- The mechanism for extending deadlines on streaming handlers.
- How `last_used_at` is throttled.
- The cursor encoding for object listing.
- Whether `last_used_at` writes and event publishing run inline or in a goroutine.

## Acceptance

- An admin can create a group and issue a key using only the root credentials.
- With that key, a client can create a bucket, upload an object twice, list both versions, and download each version through its own read URL with the matching bytes and checksum.
- A key from another group, a revoked key, or no key cannot list, upload or delete any of it; read URLs work for anyone who holds them.
- `docker compose up` runs the API with object bytes persisted in the `object_data` volume.
- `/health` keeps working. `go test ./...` passes.
