package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/pgvector/pgvector-go"
)

// Chunk stores an object's semantic-search text and optional embedding.
type Chunk struct {
	ent.Schema
}

func (Chunk) Fields() []ent.Field {
	return []ent.Field{
		field.Int("position").NonNegative(),
		field.Text("content").NotEmpty(),
		field.Other("embedding", pgvector.Vector{}).Optional().SchemaType(map[string]string{
			dialect.Postgres: "vector",
		}),
	}
}

func (Chunk) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("object", Object.Type).Ref("chunks").Unique().Required(),
	}
}

// The HNSW index on embedding is added once the embedding dimension is chosen;
// pgvector cannot index an unsized vector column.
func (Chunk) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("position").Edges("object").Unique(),
	}
}
