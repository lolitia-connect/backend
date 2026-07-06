package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
)

type ServerConfigOverride struct {
	ent.Schema
}

func (ServerConfigOverride) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "server_config_overrides"}}
}

func (ServerConfigOverride) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("id").StorageKey("id").Immutable(),
		field.Int64("server_id").StorageKey("server_id"),
		field.String("ip_strategy").StorageKey("ip_strategy").MaxLen(32).Optional().Nillable(),
		field.String("dns").StorageKey("dns").Optional().Nillable(),
		field.String("block").StorageKey("block").Optional().Nillable(),
		field.String("outbound").StorageKey("outbound").Optional().Nillable(),
		field.Time("created_at").StorageKey("created_at").Default(time.Now).Immutable(),
		field.Time("updated_at").StorageKey("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}
