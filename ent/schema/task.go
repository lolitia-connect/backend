package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
)

// Task holds the schema definition for the task table.
type Task struct {
	ent.Schema
}

func (Task) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "task"},
	}
}

func (Task) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("id").StorageKey("id").Immutable(),
		field.Int8("type").StorageKey("type"),
		field.String("scope").StorageKey("scope").Optional(),
		field.String("content").StorageKey("content").Optional(),
		field.Int8("status").StorageKey("status").Default(0),
		field.String("errors").StorageKey("errors").Optional(),
		field.Uint64("total").StorageKey("total").Default(0),
		field.Uint64("current").StorageKey("current").Default(0),
		field.Time("created_at").StorageKey("created_at").Default(time.Now).Immutable(),
		field.Time("updated_at").StorageKey("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}
