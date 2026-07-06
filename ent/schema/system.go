package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// System holds the schema definition for the system table.
type System struct {
	ent.Schema
}

func (System) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "system"},
	}
}

func (System) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("id").StorageKey("id").Immutable(),
		field.String("category").StorageKey("category").Default(""),
		field.String("key").StorageKey("key").Default(""),
		field.String("value").StorageKey("value"),
		field.String("type").StorageKey("type").Default(""),
		field.String("desc").StorageKey("desc"),
		field.Time("created_at").StorageKey("created_at").Default(time.Now).Immutable(),
		field.Time("updated_at").StorageKey("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

func (System) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("key").Unique(),
	}
}
