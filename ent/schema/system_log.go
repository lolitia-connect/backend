package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// SystemLog holds the schema definition for the system_logs table.
type SystemLog struct {
	ent.Schema
}

func (SystemLog) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "system_logs"},
	}
}

func (SystemLog) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("id").StorageKey("id").Immutable(),
		field.Uint8("type").StorageKey("type").Default(0),
		field.String("date").StorageKey("date").Optional(),
		field.Int64("object_id").StorageKey("object_id").Default(0),
		field.String("content").StorageKey("content"),
		field.Time("created_at").StorageKey("created_at").Default(time.Now).Immutable(),
	}
}

func (SystemLog) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("type"),
		index.Fields("object_id"),
		index.Fields("type", "date"),
	}
}
