package schema

import (
	"time"

	"archio/internal/schemaorg"
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
)

// Bucket groups objects and is represented by schema.org's DataCatalog concept.
type Bucket struct {
	ent.Schema
}

func (Bucket) Fields() []ent.Field {
	return []ent.Field{
		field.String("name").NotEmpty().Unique(),
		field.String("schema_type").Default(schemaorg.DataCatalog),
		field.String("description").Optional(),
		field.Time("created_at").Default(time.Now).Immutable(),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

func (Bucket) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("objects", Object.Type),
		edge.From("group", Group.Type).Ref("buckets").Unique().Required(),
	}
}
