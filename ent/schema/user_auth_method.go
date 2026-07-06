package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
)

// UserAuthMethod holds the schema definition for the user_auth_methods table.
type UserAuthMethod struct {
	ent.Schema
}

func (UserAuthMethod) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "user_auth_methods"},
	}
}

func (UserAuthMethod) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("id").StorageKey("id").Immutable(),
		field.Int64("user_id").StorageKey("user_id"),
		field.String("auth_type").StorageKey("auth_type").MaxLen(255),
		field.String("auth_identifier").StorageKey("auth_identifier").MaxLen(255),
		field.Bool("verified").StorageKey("verified").Default(false),
		field.Time("created_at").StorageKey("created_at").Default(time.Now).Immutable(),
		field.Time("updated_at").StorageKey("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}
