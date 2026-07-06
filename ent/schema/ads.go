package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
)

// Ads holds the schema definition for the ads table.
type Ads struct {
	ent.Schema
}

func (Ads) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "ads"},
	}
}

func (Ads) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("id").StorageKey("id").Immutable(),
		field.String("title").StorageKey("title").Default(""),
		field.String("type").StorageKey("type").Default(""),
		field.String("content").StorageKey("content").Optional(),
		field.String("description").StorageKey("description").Optional(),
		field.String("target_url").StorageKey("target_url").Default(""),
		field.Time("start_time").StorageKey("start_time").Optional(),
		field.Time("end_time").StorageKey("end_time").Optional(),
		field.Int("status").StorageKey("status").Default(0),
		field.Time("created_at").StorageKey("created_at").Default(time.Now).Immutable(),
		field.Time("updated_at").StorageKey("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}
