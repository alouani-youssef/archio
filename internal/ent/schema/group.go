package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"github.com/google/uuid"
)

// Group owns buckets and API keys.
type Group struct {
	ent.Schema
}

func (Group) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New),
		field.String("name").NotEmpty().Unique(),
		field.Time("created_at").Default(time.Now).Immutable(),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

func (Group) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("buckets", Bucket.Type),
		edge.To("api_keys", ApiKey.Type),
	}
}
