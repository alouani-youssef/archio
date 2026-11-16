package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// ExactSearchValue stores a normalized scalar value for exact object lookup.
type ExactSearchValue struct {
	ent.Schema
}

func (ExactSearchValue) Fields() []ent.Field {
	return []ent.Field{
		field.String("property_iri").NotEmpty(),
		field.String("normalized_value").NotEmpty(),
		field.String("value_type").NotEmpty(),
	}
}

func (ExactSearchValue) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("object", Object.Type).Ref("exact_search_values").Unique().Required(),
	}
}

func (ExactSearchValue) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("property_iri", "normalized_value").Edges("object"),
	}
}
