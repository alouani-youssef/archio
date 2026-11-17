package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/google/uuid"
)

// ObjectVersion is one immutable stored payload of an object.
type ObjectVersion struct {
	ent.Schema
}

func (ObjectVersion) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New),
		field.Int64("size").NonNegative(),
		field.String("checksum_sha256").NotEmpty(),
		field.String("content_type").NotEmpty(),
		field.String("storage_key").NotEmpty(),
		field.Time("created_at").Default(time.Now).Immutable(),
	}
}

func (ObjectVersion) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("object", Object.Type).Ref("versions").Unique().Required(),
	}
}

func (ObjectVersion) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("created_at").Edges("object"),
	}
}
