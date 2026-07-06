package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
)

// Announcement holds the schema definition for the announcement table.
type Announcement struct {
	ent.Schema
}

func (Announcement) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "announcement"},
	}
}

func (Announcement) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("id").StorageKey("id").Immutable(),
		field.String("title").StorageKey("title").Default(""),
		field.String("content").StorageKey("content").Optional(),
		field.Bool("show").StorageKey("show").Default(false),
		field.Bool("pinned").StorageKey("pinned").Default(false),
		field.Bool("popup").StorageKey("popup").Default(false),
		field.Time("created_at").StorageKey("created_at").Default(time.Now).Immutable(),
		field.Time("updated_at").StorageKey("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}
