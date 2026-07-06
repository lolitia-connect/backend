package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
)

type UserDevice struct {
	ent.Schema
}

func (UserDevice) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "user_device"}}
}

func (UserDevice) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("id").StorageKey("id").Immutable(),
		field.String("ip").StorageKey("ip").MaxLen(255),
		field.Int64("user_id").StorageKey("user_id"),
		field.String("user_agent").StorageKey("user_agent").Optional().Nillable(),
		field.String("identifier").StorageKey("identifier").MaxLen(255).Default(""),
		field.String("short_code").StorageKey("short_code").MaxLen(255).Default(""),
		field.Bool("online").StorageKey("online").Default(false),
		field.Bool("enabled").StorageKey("enabled").Default(true),
		field.Time("created_at").StorageKey("created_at").Default(time.Now).Immutable(),
		field.Time("updated_at").StorageKey("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}
