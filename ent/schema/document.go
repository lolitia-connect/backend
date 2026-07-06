package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
)

// Document holds the schema definition for the document table.
type Document struct {
	ent.Schema
}

func (Document) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "document"},
	}
}

func (Document) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("id").StorageKey("id").Immutable(),
		field.String("title").StorageKey("title").Default(""),
		field.String("content").StorageKey("content").Optional(),
		field.String("tags").StorageKey("tags").Default(""),
		field.Bool("show").StorageKey("show").Default(true),
		field.Time("created_at").StorageKey("created_at").Default(time.Now).Immutable(),
		field.Time("updated_at").StorageKey("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}
