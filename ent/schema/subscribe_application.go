package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
)

// SubscribeApplication holds the schema definition for the subscribe_application table.
type SubscribeApplication struct {
	ent.Schema
}

func (SubscribeApplication) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "subscribe_application"},
	}
}

func (SubscribeApplication) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("id").StorageKey("id").Immutable(),
		field.String("name").StorageKey("name").Default(""),
		field.String("icon").StorageKey("icon").Optional(),
		field.String("description").StorageKey("description").Optional(),
		field.String("scheme").StorageKey("scheme").Default(""),
		field.String("user_agent").StorageKey("user_agent").Default(""),
		field.Bool("is_default").StorageKey("is_default").Default(false),
		field.String("subscribe_template").StorageKey("subscribe_template").Optional(),
		field.String("output_format").StorageKey("output_format").Default("yaml"),
		field.String("download_link").StorageKey("download_link"),
		field.Time("created_at").StorageKey("created_at").Default(time.Now).Immutable(),
		field.Time("updated_at").StorageKey("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}
