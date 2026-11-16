package schema

import (
	"time"

	"archio/internal/schemaorg"
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// MetaData stores flexible schema.org PropertyValue records for an object.
type MetaData struct {
	ent.Schema
}

func (MetaData) Fields() []ent.Field {
	return []ent.Field{
		field.String("schema_type").Default(schemaorg.PropertyValue),
		field.String("property_iri").NotEmpty(),
		field.JSON("value", map[string]any{}).Optional(),
		field.Time("created_at").Default(time.Now).Immutable(),
	}
}

func (MetaData) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("object", Object.Type).Ref("metadata").Unique().Required(),
	}
}

func (MetaData) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("property_iri").Edges("object"),
	}
}
