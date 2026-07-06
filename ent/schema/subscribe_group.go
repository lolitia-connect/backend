package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
)

type SubscribeGroup struct {
	ent.Schema
}

func (SubscribeGroup) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "subscribe_group"}}
}

func (SubscribeGroup) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("id").StorageKey("id").Immutable(),
		field.String("name").StorageKey("name").MaxLen(255).Default(""),
		field.String("description").StorageKey("description").Optional(),
		field.Time("created_at").StorageKey("created_at").Default(time.Now).Immutable(),
		field.Time("updated_at").StorageKey("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}
