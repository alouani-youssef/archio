# Ent and schema.org Storage Design

## Intent

Add modular application startup and Ent-based PostgreSQL persistence for Archio's core bucket/object records, with schema.org identifiers for interoperable types and properties, plus separate entities for semantic and exact search. Expose a readiness endpoint that checks the Ent/PostgreSQL connection.

## Goals

- Configure Ent to connect to the existing PostgreSQL configuration.
- Keep process startup, infrastructure lifecycle, feature modules, and HTTP transport separated by responsibility.
- Open and close the Ent client through one lifecycle owner, with context-aware ping/readiness support.
- Expose `/health` as a readiness endpoint that returns success only when PostgreSQL is reachable.
- Define core Bucket, Object, and MetaData entities and their relationships.
- Use schema.org IRIs for object types and metadata property identifiers, with JSONB for flexible property values.
- Define separate search entities related to Object: semantic chunks with embeddings and exact-search field/value records.
- Provide database indexes for core lookups, exact value matches, and nearest-neighbor chunk retrieval.
- Keep generated Ent code and the schema definitions reproducible from the repository.

## Non-goals

- Implementing bucket/object HTTP endpoints or ingestion pipelines.
- Downloading or vendoring the complete schema.org vocabulary.
- Adding tenant or authorization behavior not present in the current application.
- Selecting an embedding model or generating embeddings.

## Proposed model

- `Bucket` is a named container for `Object` records. It has a stable identifier and timestamps.
- `Object` belongs to one bucket, has a stable identifier and a schema.org type IRI (for example, `https://schema.org/Article`), and carries flexible schema.org properties in JSONB.
- `MetaData` is part of the core object model: it records schema.org property IRIs and typed JSONB values, allowing repeated properties without baking the full vocabulary into SQL columns.
- `Chunk` is a semantic-search entity belonging to an object. It stores chunk order, text, and an optional embedding vector. A pgvector HNSW index supports cosine-distance search; relational indexes support ordered chunk retrieval.
- `ExactSearchValue` is an exact-search entity belonging to an object. It stores the source property IRI, a normalized scalar value, and its value type. A composite B-tree index on property and normalized value supports equality lookups; an object index resolves matches to their source object.

Schema.org remains an external vocabulary represented by stable IRIs. The model may define commonly used types/properties as Go constants, but must not imply that those constants exhaust or replace schema.org.

## Approaches

1. **Schema.org graph:** represent every type, property, and value as generic graph nodes and edges. This most closely mirrors the vocabulary but makes common bucket/object queries and application code more complex.
2. **Fixed relational schema:** add SQL columns for a selected set of schema.org terms and maintain indexes on those columns. This is easy to query but makes new and repeated schema.org properties require schema changes.
3. **Core plus search entities (recommended):** relational Ent entities for buckets, objects, and metadata; schema.org IRIs identify types/properties; JSONB holds flexible values; dedicated chunk and exact-value entities support the two search modes. This keeps core records independent from search-specific storage and leaves the vocabulary extensible.

This approach is proposed because the repository already separates bucket module responsibilities and uses PostgreSQL with pgvector, while currently having no persisted domain model. It also reflects the requirement that semantic and exact search data live in entities associated with objects rather than defining the core object itself.

## Constraints and implementation choices

- Use the existing `DB_HOST`, `DB_PORT`, `DB_USERNAME`, `DB_PASSWORD`, and `DB_DATABASE` settings.
- Use PostgreSQL and the pgvector extension provided by the current Compose stack.
- Keep schema definitions under the Ent schema directory and document the code-generation command.
- The exact Ent-compatible vector field/type and migration/index representation must be chosen against the Ent and pgvector Go APIs available at implementation time; generated code must not be hand-edited.
- Database migration should create the `vector` extension before the vector column/index is applied.
- Application startup should fail clearly if PostgreSQL connection or schema setup fails, and close the Ent client on shutdown.
- The health endpoint should return HTTP 503 when PostgreSQL is not ready and should not expose connection strings or credentials.

## Open implementation decisions

- Embedding dimension is not specified. The initial schema should make it configurable or leave the vector column/index dimension unset until the application selects an embedding model; a fixed-size HNSW index cannot be finalized without that choice.
- Unique identifier rules and soft-delete behavior are not specified. Use bucket-scoped object identifiers and avoid adding soft deletion unless required by existing code.

## Acceptance

- Ent client configuration uses the existing database configuration and supports schema migration.
- Ent schemas express the core relationships and separate search entities, including exact property/value matching, chunk ordering, and nearest-neighbor vector search support.
- schema.org identifiers are represented consistently and can include terms beyond any built-in examples.
- Repository documentation explains Ent code generation and local database startup.
