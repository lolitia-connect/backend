package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// AuthMethod holds the schema definition for the auth_method table.
type AuthMethod struct {
	ent.Schema
}

func (AuthMethod) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "auth_method"},
	}
}

func (AuthMethod) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("id").StorageKey("id").Immutable(),
		field.String("method").StorageKey("method").Default(""),
		field.String("config").StorageKey("config"),
		field.Bool("enabled").StorageKey("enabled").Default(false),
		field.Time("created_at").StorageKey("created_at").Default(time.Now).Immutable(),
		field.Time("updated_at").StorageKey("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

func (AuthMethod) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("method").Unique(),
	}
}
