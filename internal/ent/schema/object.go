package schema

import (
	"time"

	"archio/internal/schemaorg"
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// Object is a schema.org Thing stored in a bucket.
type Object struct {
	ent.Schema
}

func (Object) Fields() []ent.Field {
	return []ent.Field{
		field.String("identifier").NotEmpty(),
		field.String("schema_type").Default(schemaorg.Thing),
		field.String("name").Optional(),
		field.String("description").Optional(),
		field.Time("created_at").Default(time.Now).Immutable(),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

func (Object) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("bucket", Bucket.Type).Ref("objects").Unique().Required(),
		edge.To("metadata", MetaData.Type),
		edge.To("chunks", Chunk.Type),
		edge.To("exact_search_values", ExactSearchValue.Type),
		edge.To("versions", ObjectVersion.Type),
		edge.To("current_version", ObjectVersion.Type).Unique(),
	}
}

func (Object) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("identifier").Edges("bucket").Unique(),
	}
}
