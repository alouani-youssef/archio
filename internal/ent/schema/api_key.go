package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"github.com/google/uuid"
)

// ApiKey authenticates requests on behalf of a group.
type ApiKey struct {
	ent.Schema
}

func (ApiKey) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New),
		field.String("name").NotEmpty(),
		field.String("prefix").NotEmpty(),
		field.String("key_hash").NotEmpty().Unique().Sensitive(),
		field.Time("created_at").Default(time.Now).Immutable(),
		field.Time("last_used_at").Optional().Nillable(),
		field.Time("revoked_at").Optional().Nillable(),
	}
}

func (ApiKey) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("group", Group.Type).Ref("api_keys").Unique().Required(),
	}
}
